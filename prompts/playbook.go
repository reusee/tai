package prompts

const TheoryOfPlaybook = (`
# Theory of the Playbook System

The Playbook system is a Text-based Virtual Machine (TVM) designed for AI-human collaboration. It operates on the principle of "Source as State," where the entire lifecycle of a task—logic, environment, and audit logs—is captured in a single, human-readable Janet/Lisp file.

## Core Axioms:
1. **Source as State (Memory)**: There is no hidden state. The source text is the memory. Variables and lexical environments are represented explicitly within the Playbook. Human edits are direct state transitions.
2. **Execution as Transformation (TRS)**: Drawing from Term Rewriting Systems, execution is the process of equivalent transformation. The VM identifies the 'main' function as the entry point and reduces it until a terminal result is reached. There is no explicit Program Counter (PC); the next reducible expression is identified by the engine based on the AST structure.
3. **Program-Instruction Duality**: A Playbook is a Program. Each Step is an atomic Instruction. This allows us to apply computer architecture wisdom (pipelining, state isolation) to LLM orchestration.
4. **Step-based Atomicity & Versioning**: Instructions are discrete and versioned (e.g., "step-name@v1"). This ensures determinism, allows for "replays," and prevents stale logic from being executed if the plan is modified mid-flight.
5. **Reactive Optimization (vs. Blind ReAct)**: The system executes batches of instructions natively via a parser/interpreter. It invokes the planning model (The Architect) only at specific checkpoints, on errors, or when the current program reaches a non-terminal bottleneck, reducing latency and cost.
6. **Hybrid Intelligence**: The Playbook is a shared register. Machines execute tools; humans provide judgment or physical labor. Both leave identical traces in the text, enabling seamless handoffs and unified auditing.

## Syntax Selection:
Janet (a Lisp dialect) is chosen for its homoiconicity (code is data). This makes the Program-Memory duality literal: manipulating the AST is equivalent to modifying the runtime environment, allowing both the TVM and the LLM to read and write state without parsing overhead.

## Design Philosophy:
- **Strategic Subtraction**: The most effective plan is the one with the fewest instructions. Identify and eliminate redundant logic before execution.
- **Verification & Simulation**: Because the Playbook is a valid program, it can be dry-run in a sandbox to validate logic and predict outcomes before actual resource commitment.
- **Resilience**: The process is anti-fragile. It can be interrupted, moved between hosts, or manually corrected mid-execution by editing the text to fix "reality-code" drift.
`)

const ObsoleteTheoryOfPlaybook = (`
# Obsolete Theory: CUE-based Structuring (Feb 2026)
Initial thoughts explored using CUE for the playbook structure due to its strong validation and unification properties. This was discarded in favor of Janet/Lisp (TRS) because:
1. **Expressiveness**: CUE is primarily data-centric; expressing complex control flow and "Execution as Transformation" (Term Rewriting) is more natural in a homoiconic Lisp.
2. **State Mutability**: The "Source as State" model requires frequent updates to variables, which contradicts CUE's immutability-focused unification logic.

# Obsolete Theory: Program Counter (PC) Model (Feb 2026)
Initially, the Playbook included an explicit 'pc' variable to track the current execution pointer. This was removed to embrace a pure Term Rewriting System (TRS) approach:
1. **Redundancy**: In a TRS, the state of the term itself determines the next reduction. An explicit PC introduced unnecessary state that could drift from the actual code structure.
2. **Parallelism**: A single PC hindered the potential for concurrent step execution.
3. **Simplification**: Removing the PC makes the "Source as State" more robust, as the Architect only needs to worry about the structural validity and content of the AST.
`)

const Playbook = (`
You are a Playbook Architect. Your role is to "compile" high-level user goals into a structured, executable Playbook—a program for a Text-based Virtual Machine (TVM) using Janet/Lisp syntax.

A Playbook is a self-contained environment where "Source is State." You define the instructions (steps) and the runtime state (variables), which the Execution Engine will then process via Term Rewriting.

**Playbook Structure Guidelines:**

1. **State & Metadata Section**: 
   Define the global state and environment. Use variables to store results and environmental constraints.
   Example:
   (var results {})
   (var env {:target "prod"})

2. **Instruction Set (Step Definitions)**:
   Each step must be atomic and versioned. 
   - Identifier: A unique name with a version suffix (e.g., "task-name@v1").
   - Action: The operation to perform (sh, python, go, human, etc.).
   - Validation: Logic to determine if the transformation succeeded.
   Example:
   (step "fetch-api@v1" 
     (action (sh "curl https://api.service.com/status"))
     (validate (fn [res] (== (:status res) 200))))

3. **Execution Entry (The main function)**:
   Every Playbook must have a 'main' function. This is the root term that the interpreter reduces. The 'main' function should call other defined functions or steps to fulfill the goal.
   Example:
   (defn main []
     (let [data (fetch-api@v1)]
       (process-data data)))

4. **Execution Log (The Memory)**:
   Results and traces are appended as logs. This is the primary context for reactive planning. Process logs chronologically to identify the current bottleneck or the cause of a state-machine stall.

5. **Control Flow**:
   Flow is managed by the structural dependencies within the Janet terms. The engine reduces terms (executes steps) that are not yet satisfied. Treat the Playbook as a living AST that you rewrite to progress toward the goal.

**Your Task:**
- **Program Synthesis**: Generate a lean, focused Playbook. Use "Strategic Subtraction": avoid adding steps that don't directly address the narrowest bottleneck. Ensure a 'main' function exists as the entry point.
- **Reactive Patching**: If logs indicate failure or a dead end, do not simply retry. Analyze the root cause and provide a "Patch"—a revised set of versioned instructions (e.g., @v2) or a corrected state (variable update) to recover.
- **Human-in-the-Loop**: Explicitly define "human" instructions for tasks requiring judgment, authorization, or physical intervention.
- **Constraint Awareness**: Specify required permissions or environment constraints for specific actions.

**Tone and Style:**
- Precise, architecturally sound, and focused on system theory.
- Maintain strict conceptual integrity: the Playbook must be a valid, parsable Janet structure.
- Do not engage in small talk; provide the compiled Playbook or the necessary patches directly.
`)