package main

import (
	"context"
	"iter"
	"time"

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
  Output tab title renders "processing <kind> <elapsed>..." while the
  field is set, the elapsed fragment being the stopwatch reading of
  blockProcessingStart; the label precedence is finished > handoff >
  processing > generating, with processing ranked above generating
  because a request that ends without a finish node leaves the generating
  hint on while a component still works. BlockProcessingEnd and genEnd
  clear the state and the start moment together, so a session that ends
  mid-processing leaves no hint stuck on.
- The stopwatch advances without unrelated events: the session loop wakes
  only on keys, updates, and resizes, and a component's work may run for
  minutes with no event to wake it. The session's ticker notifies the loop
  once per blockProcessingTick while the field is set, and the fragment
  renders in the Tree tab rows' "+0:07" form (formatTreeElapsed), so both
  stopwatches read alike.
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

// blockProcessingTick is the wake interval of the Output tab's processing
// stopwatch: the elapsed fragment renders whole seconds, so one wake per
// second keeps it current. See TheoryOfTUIBlockProcessing.
const blockProcessingTick = time.Second

// BlockProcessingStart reports the kind of the component whose block
// processing is starting, and records the stopwatch's start moment so the
// Output tab title can render the elapsed time. It is called on the
// generation goroutine by the observer wrapper. See
// TheoryOfTUIBlockProcessing.
func (t *TUI) BlockProcessingStart(kind string) {
	t.mu.Lock()
	t.blockProcessing = kind
	t.blockProcessingStart = time.Now()
	t.mu.Unlock()
	t.notify()
}

// BlockProcessingEnd reports that the component's block processing has
// ended, and clears the stopwatch's start moment together with the hint.
// See TheoryOfTUIBlockProcessing.
func (t *TUI) BlockProcessingEnd() {
	t.mu.Lock()
	t.blockProcessing = ""
	t.blockProcessingStart = time.Time{}
	t.mu.Unlock()
	t.notify()
}

// tickBlockProcessing wakes the session loop once per blockProcessingTick
// while a component's block processing is active, so the Output tab's
// processing stopwatch advances without unrelated events. It returns when
// stop is closed, which Run does when the session ends. See
// TheoryOfTUIBlockProcessing.
func (t *TUI) tickBlockProcessing(stop <-chan struct{}) {
	ticker := time.NewTicker(blockProcessingTick)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			t.mu.Lock()
			active := t.blockProcessing != ""
			t.mu.Unlock()
			if active {
				t.notify()
			}
		}
	}
}

// blockProcessingElapsedLocked returns the elapsed duration since the
// current component's block processing started, or zero when no
// processing is active. The caller holds t.mu. See
// TheoryOfTUIBlockProcessing.
func (t *TUI) blockProcessingElapsedLocked() time.Duration {
	if t.blockProcessing == "" {
		return 0
	}
	return time.Since(t.blockProcessingStart)
}
