package taitape

const Theory = `
# TaiTape System Theory

TaiTape is an autonomous execution architecture based on a "Tape-based Virtual Machine" model, mimicking the fundamental Von Neumann architecture using structured text.

## 1. Core Architecture: The Tape
The system state is entirely contained within a structured file (the "Tape"), serving as the unified memory and program storage.
- **Program (Steps)**: A sequence of discrete instructions. Each step functions as a "machine instruction" containing an action, status, and metadata.
- **PC (Program Counter)**: A register-like index pointing to the next step to execute.
- **Globals (RAM)**: A persistent key-value store acting as the system's primary memory. Values must be JSON-serializable to maintain persistence.
- **Logs (Audit Trail)**: A chronological record of execution details, providing a read-only history of the VM's operations.

## 2. Design Philosophy: Code as State
- **Decoupling Plan and Execution**: Plans are "compiled" (generated) by LLMs or humans as sequences of steps. The runner is a deterministic engine that simply executes the Tape.
- **Transparency & Persistence**: The Tape is a physical artifact. Unlike hidden LLM contexts, it is inspectable, auditable, and editable by both humans and machines.
- **Resilience**: The file is the checkpoint. Execution can be interrupted, moved between machines, or resumed after a crash simply by reading the PC and Globals from the Tape.

## 3. Operational Mechanics (The Fetch-Execute Cycle)
1. **Lock**: The runner acquires a file lock on the Tape to ensure atomic access.
2. **Fetch**: Read the instruction (Step) at the current PC. 
3. **Dispatch**: Check status. If "completed", increment PC and repeat. If "paused" or "failed", wait for external intervention.
4. **Transition**: Set status to "running" and commit to disk. This marks the intent to execute and enables recovery if the runner crashes.
5. **Execute**: Run the action (Shell, TaiGo, TaiPy, or Control flow).
6. **Sync**: Capture outputs, update Globals (RAM), and determine the next PC.
7. **Commit**: Save the updated Tape (PC incremented or jumped) and release the lock.

## 4. Instruction Set & Control Flow
- **Standard Actions**: shell, taigo, taipy.
- **Control Actions**: 
    - nop: No operation, often used as a label or placeholder.
    - jump: Explicitly set PC to a target index or label name/ID.
    - wait: Pause execution until the step is manually marked as completed or pending.
    - exit: Gracefully terminate execution by setting PC out of bounds.

## 5. Human-Machine Collaboration
The Tape is the "Boundary Object". Humans can intervene by modifying the Tape (fixing a step, updating a global) while the runner is paused or even between steps. This allows for true "Human-in-the-Loop" without breaking the execution flow, as the runner always re-fetches the state from disk.
`
