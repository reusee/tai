package main

import (
	"context"
	"iter"

	"github.com/reusee/tai/components"
	"github.com/reusee/tai/pipeline"
	"github.com/reusee/tai/tree"
)

const TheoryOfTUIBlockProcessing = `
Block processing display theory (cmd/tai):
- The processing phase is invisible in the session tree until it ends: a
  component writes its block-result nodes only after its work returns, so
  a long go-test run, a symbol resolution, or a context fetch shows
  nothing. The display needs an observer that fires when the work STARTS;
  the tree alone cannot drive the hint.
- The observation lives in the display decorator, not in the pipeline or
  the components packages: observedComponents rewraps each processable
  component's Process function to report its kind around the original
  call, preserving Kind, Process, Compute, and PromptSection, so the
  component set the pipeline runs is behaviorally identical. The
  pipeline's component loop stays untouched.
- A prompt-only component (nil Process) is copied unchanged, and a
  component with no matching blocks never reaches its Process, so the
  observer fires exactly when ProcessComponents runs a component's work.
- The observer's callbacks run on the generation goroutine, one component
  at a time — ProcessComponents runs components sequentially — so the
  TUI's blockProcessing field names the component currently working. The
  Output tab title renders "processing <kind>..." while the field is set;
  the label precedence is finished > handoff > processing > generating,
  with processing ranked above generating because a request that ends
  without a finish node leaves the generating hint on while a component
  still works. genEnd clears the state, so a session that ends
  mid-processing leaves no hint stuck on.
- The decorator travels with the scope like the output observer: each
  loop scope re-evaluates Module.Run and re-applies it, so a goal loop's
  component processing reports through the same TUI. See
  TheoryOfTUIDisplayFork and pipeline.TheoryOfRunDecorators.
`

// blockProcessingObserver reports the lifecycle of one component's block
// processing: the component's kind before its work starts, and the end
// after it returns. The display decorator wraps the session's component
// set with the TUI as the observer, so the Output tab title names the
// component the user currently waits on. See TheoryOfTUIBlockProcessing.
type blockProcessingObserver interface {
	BlockProcessingStart(kind string)
	BlockProcessingEnd()
}

// observedComponents returns a copy of the component set whose processable
// components report their processing lifecycle to the observer. Every
// other component field is preserved, so the set behaves identically.
// See TheoryOfTUIBlockProcessing.
func observedComponents(comps components.ComponentSet, observer blockProcessingObserver) components.ComponentSet {
	if observer == nil {
		return comps
	}
	observed := make(components.ComponentSet, len(comps))
	for i, comp := range comps {
		process := comp.Process
		if process == nil {
			observed[i] = comp
			continue
		}
		kind := comp.Kind
		comp.Process = func(ctx context.Context, pctx *components.ProcessContext) components.ProcessResult {
			observer.BlockProcessingStart(kind)
			defer observer.BlockProcessingEnd()
			return process(ctx, pctx)
		}
		observed[i] = comp
	}
	return observed
}

// withTUIBlockProcessing wraps a run so the session's components report
// their block processing to the TUI: the Output tab title names the
// component currently working. It is a run decorator, applied by
// Module.Run inside its provider, so every goal loop's component
// processing reports through the same display. See
// TheoryOfTUIBlockProcessing and TheoryOfTUIDisplayFork.
func withTUIBlockProcessing(run pipeline.Run, tui *TUI) pipeline.Run {
	return func(ctx context.Context, opts pipeline.RunOptions, result *pipeline.Result) iter.Seq2[*tree.Tree, error] {
		opts.Components = observedComponents(opts.Components, tui)
		return run(ctx, opts, result)
	}
}

// BlockProcessingStart reports the kind of the component whose block
// processing is starting. It is called on the generation goroutine by the
// observer wrapper. See TheoryOfTUIBlockProcessing.
func (t *TUI) BlockProcessingStart(kind string) {
	t.mu.Lock()
	t.blockProcessing = kind
	t.mu.Unlock()
	t.notify()
}

// BlockProcessingEnd reports that the component's block processing has
// ended. See TheoryOfTUIBlockProcessing.
func (t *TUI) BlockProcessingEnd() {
	t.mu.Lock()
	t.blockProcessing = ""
	t.mu.Unlock()
	t.notify()
}
