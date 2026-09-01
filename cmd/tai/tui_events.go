package main

// TheoryOfEventTree documents the cmd/tai side of the Events tab's
// event tree; the tree mechanism itself lives in taiui. See
// taiui.TheoryOfEventTree.
const TheoryOfEventTree = `
Events tab integration theory (cmd/tai):
- The Events tab renders the pipeline event stream through
  taiui.EventTree: handleEvent renders each pipeline.Run event into
  lines with eventLines and files them into the tree by the event's
  (loop, sequence, parent) identity, so the tab renders the stream in
  tree order however the events arrive.
- A pipeline.EventHandoff event with a summary body is the expandable
  node: it collapses to its header plus the expand hint, so a long
  recovery note does not flood the tail view. Enter maps to
  ToggleLastExpanded; a left press maps the screen cell to a display
  row (dropping the label strip, re-adding the scroll offset): a press
  on the attempt-start line's 👉 jump marker jumps the Output tab to
  the output section that attempt wrote (see
  TheoryOfTUIOutputSections), and every press calls ToggleAtRow.
- Every event records the duration from the TUI session's start to its
  arrival as the node's elapsed time.
`
