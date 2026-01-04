package codes

import (
	"context"
	"fmt"
	"io"
	"math"
	"regexp"
	"sort"
	"strconv"
	"sync"

	"github.com/reusee/dscope"
	"github.com/reusee/tai/cmds"
	"github.com/reusee/tai/codes/codetypes"
	"github.com/reusee/tai/generators"
	"github.com/reusee/tai/logs"
	"github.com/reusee/tai/phases"
)

type rankFileInfo struct {
	content generators.Part
	text    string
	tokens  int
	score   int
}

type ActionRank struct {
	ActionArgument   dscope.Inject[ActionArgument]
	GetPlanGenerator dscope.Inject[GetPlanGenerator]
	GetCodeGenerator dscope.Inject[GetCodeGenerator]
	CodeProvider     dscope.Inject[codetypes.CodeProvider]
	Patterns         dscope.Inject[Patterns]
	BuildGenerate    dscope.Inject[phases.BuildGenerate]
	BuildChat        dscope.Inject[phases.BuildChat]
	Logger           dscope.Inject[logs.Logger]
}

var _ Action = ActionRank{}

func (Module) ActionRank(
	inject dscope.InjectStruct,
) (ret ActionRank) {
	inject(&ret)
	return
}

func (a ActionRank) Name() string {
	return "rank"
}

func (a ActionRank) DefineCmds() {
	cmds.Define(a.Name(), cmds.Func(func(args *string) {
		actionNameFlag = a.Name()
		actionArgumentFlag = ActionArgument(*args)
	}).Desc("rank files by relevance and process"))
}

func (a ActionRank) InitialGenerator() (generators.Generator, error) {
	return a.GetCodeGenerator()()
}

func (a ActionRank) InitialPhase(cont phases.Phase) phases.Phase {
	return func(ctx context.Context, state generators.State) (phases.Phase, generators.State, error) {
		m1, err := a.GetPlanGenerator()()
		if err != nil {
			return nil, nil, err
		}
		m2, err := a.GetCodeGenerator()()
		if err != nil {
			return nil, nil, err
		}

		goal := string(a.ActionArgument())
		patterns := a.Patterns()
		provider := a.CodeProvider()

		allParts, err := provider.Parts(math.MaxInt, m1.CountTokens, patterns)
		if err != nil {
			return nil, nil, err
		}
		a.Logger().Info("initial", "parts", len(allParts))

		var files []*rankFileInfo
		var mu sync.Mutex
		var wg sync.WaitGroup
		for _, part := range allParts {
			text, ok := part.(generators.Text)
			if !ok {
				continue
			}
			wg.Add(1)
			go func(p generators.Part, content string) {
				defer wg.Done()
				tokens, _ := m1.CountTokens(content)
				mu.Lock()
				files = append(files, &rankFileInfo{
					content: p,
					text:    content,
					tokens:  tokens,
				})
				mu.Unlock()
			}(part, string(text))
		}
		wg.Wait()

		m1Args := m1.Args()
		maxBatchTokens := (m1Args.ContextTokens / 2)
		if maxBatchTokens > 12000 {
			maxBatchTokens = 12000
		}
		var currentBatch []*rankFileInfo
		var currentBatchTokens int
		var batches [][]*rankFileInfo
		for _, f := range files {
			if currentBatchTokens+f.tokens > maxBatchTokens && len(currentBatch) > 0 {
				batches = append(batches, currentBatch)
				currentBatch = nil
				currentBatchTokens = 0
			}
			currentBatch = append(currentBatch, f)
			currentBatchTokens += f.tokens
		}
		if len(currentBatch) > 0 {
			batches = append(batches, currentBatch)
		}

		limit := make(chan struct{}, 8)
		for _, batch := range batches {
			wg.Add(1)
			go func(batch []*rankFileInfo) {
				defer wg.Done()
				limit <- struct{}{}
				defer func() { <-limit }()
				a.scoreBatch(ctx, m1, goal, batch)
			}(batch)
		}
		wg.Wait()

		sort.SliceStable(files, func(i, j int) bool {
			return files[i].score > files[j].score
		})

		m2Args := m2.Args()
		maxTokens := m2Args.ContextTokens
		if m2Args.MaxGenerateTokens != nil {
			maxTokens -= *m2Args.MaxGenerateTokens * 2
		}
		maxTokens -= 2000

		var selectedParts []generators.Part
		currentTokens := 0
		for _, f := range files {
			if currentTokens+f.tokens > maxTokens {
				break
			}
			selectedParts = append(selectedParts, f.content)
			currentTokens += f.tokens
		}

		a.Logger().Info("ranking results",
			"total", len(files),
			"selected", len(selectedParts),
			"tokens", currentTokens,
		)

		state, err = state.AppendContent(&generators.Content{
			Role:  "user",
			Parts: append(selectedParts, generators.Text("\n\nGoal: "+goal)),
		})
		if err != nil {
			return nil, nil, err
		}

		return a.BuildGenerate()(m2, nil)(
			a.BuildChat()(m2, nil)(cont),
		), state, nil
	}
}

func (a ActionRank) scoreBatch(ctx context.Context, m generators.Generator, goal string, files []*rankFileInfo) {
	if len(files) == 0 {
		return
	}
	mArgs := m.Args()
	maxInputTokens := mArgs.ContextTokens - 2000
	prompt := fmt.Sprintf("Goal: %s\nRate the relevance of each code snippet below from 0 (irrelevant) to 100 (critical) based on the goal.\nRespond with scores in the format 'ID: Score', one per line. No other text.\n\n", goal)
	for i, f := range files {
		content := f.text
		if f.tokens > maxInputTokens {
			content = content[:maxInputTokens*3]
		}
		prompt += fmt.Sprintf("ID %d:\n%s\n\n", i, content)
	}
	var state generators.State
	state = generators.NewPrompts(prompt, nil)
	state = generators.NewOutput(state, io.Discard, false)
	state, err := m.Generate(ctx, state, nil)
	if err != nil {
		a.Logger().Error("scoring failed", "error", err)
		return
	}
	contents := state.Contents()
	var responseText string
	for i := len(contents) - 1; i >= 0; i-- {
		if contents[i].Role == generators.RoleModel || contents[i].Role == generators.RoleAssistant {
			for _, part := range contents[i].Parts {
				if t, ok := part.(generators.Text); ok {
					responseText += string(t)
				}
			}
			break
		}
	}
	scoreMap := make(map[int]int)
	re := regexp.MustCompile(`(?:ID\s*)?(\d+):\s*(\d+)`)
	matches := re.FindAllStringSubmatch(responseText, -1)
	for _, m := range matches {
		id, _ := strconv.Atoi(m[1])
		score, _ := strconv.Atoi(m[2])
		if score > 100 {
			score = 100
		}
		scoreMap[id] = score
	}
	for i, f := range files {
		if s, ok := scoreMap[i]; ok {
			f.score = s
		}
	}
}
