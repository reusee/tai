package taitape

const Theory = `
# TaiTape System Theory

TaiTape is an autonomous execution architecture based on a "Tape-based Virtual Machine" model, mimicking the fundamental Von Neumann architecture using structured text.

## 1. Core Architecture: The Tape
The system state is entirely contained within a structured file (the "Tape"). 
- **Program (Steps)**: A sequence of discrete instructions. Each step has an ID, name, action, and status.
- **PC (Program Counter)**: An index pointing to the next instruction to execute.
- **Globals**: A persistent key-value store acting as the system's "RAM".
- **Logs**: A chronological record of execution details (the "Audit Trail"). To prevent tape bloat, logs may be pruned to a fixed history window.

## 2. Design Philosophy: Code as State
- **Decoupling Plan and Execution**: Plans are "compiled" (generated) by LLMs as a sequence of instructions. The runner is a deterministic, non-reasoning agent that simply executes the Tape.
- **Transparency & Persistence**: The Tape is a physical artifact. Unlike hidden LLM contexts, it is inspectable, auditable, and editable by both humans and machines.
- **Resilience**: The file is the checkpoint. Execution can be interrupted, moved between machines, or resumed after a crash simply by reading the PC and Globals from the Tape.

## 3. Operational Mechanics (The Fetch-Execute Cycle)
1. **Lock**: The runner acquires a file lock on the Tape.
2. **Fetch**: Read the instruction at the current PC. 
3. **Dispatch**: Check status. If "completed", increment PC and repeat. If "paused" or "failed", wait for intervention.
4. **Transition**: Set status to "running" and commit to disk. This marks the intent to execute and enables recovery if the runner crashes.
5. **Execute**: Run the action (Shell, TaiGo, TaiPy, or Control flow).
6. **Sync**: Capture outputs, update Globals, and determine the next PC.
7. **Commit**: Save the updated Tape (PC incremented or jumped) and release the lock.

## 4. Instruction Set & Control Flow
- **Standard Actions**: shell, taigo, taipy.
- **Control Actions**: 
    - nop: No operation, often used as a label marker.
    - jump: Explicitly set PC to a target index or label name/ID.
    - wait: Pause execution until a human or external agent marks the step as completed or pending.
    - exit: Gracefully terminate execution by setting PC out of bounds.
- **Labels**: Steps can have optional names used as jump targets for the "jump" action.

## 5. Human-Machine Collaboration
The Tape is the "Boundary Object". Humans can intervene by modifying the Tape (fixing a step, updating a global) while the runner is paused or even between steps. This allows for true "Human-in-the-Loop" without breaking the execution flow.
`

const ObsoleteTheory = `
(No obsolete theories for taitape yet.)
`