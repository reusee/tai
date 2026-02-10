package taido

const Theory = (`
# 1. Core Mission
Taido is a minimalist autonomous execution engine designed to automate repetitive, mechanical cognitive tasks. It is built for scenarios where the process is structured but requires a degree of LLM-driven decision-making to bridge tool outputs. A typical use case is the iterative optimization of a trading strategy, where the agent executes backtests, analyzes results, and adjusts parameters without human oversight.

# 2. Design Philosophy
# 2.1 Autonomous Execution
The system operates on a "set and forget" model. Once a goal is defined via an initial prompt, Taido executes the full lifecycle of reasoning and tool usage without human intervention. This eliminates the latency and cognitive load of interactive interfaces.

# 2.2 Minimalist Architecture
Taido eschews complex frameworks and multi-agent abstractions. It provides a lean ReAct (Thought-Action-Observation) loop. It is a coordinator, not a monolith; it relies on external tools for specialized logic, data processing, and heavy computation.

# 2.3 Functional Efficiency
The focus is on "doing" rather than "chatting." The tool is optimized for high-level leverage actions where a small amount of reasoning can trigger significant external processes.

# 2.4 Task Delegation
Complexity should be managed through decomposition. For tasks that are independent, require isolated research, or involve high-risk environment changes, the agent is encouraged to delegate the goal to a sub-agent. This keeps the primary agent's reasoning path focused and prevents context pollution.

# 3. Strategic Constraints
# 3.1 Non-Interactivity
There is no provision for mid-process user input. All necessary information, constraints, and permissions must be established in the initial prompt or defined within the tool environment.

# 3.2 Tool-Centric Problem Solving
If a problem is complex, Taido should not be taught to solve it internally. Instead, an external tool or program should be developed to solve it, and Taido should be instructed on how to invoke and interpret that tool.

# 4. Implementation Details
# 4.1 ReAct Loop
The core execution is a ReAct loop: Generate -> Execute Tools -> Observe -> Repeat. The loop is unbounded and continues until the agent explicitly signals completion (e.g., via the "Stop" tool or "Goal achieved.") or no tool calls are generated in a model response. This design trusts the LLM to manage the task horizon and termination.

The system provides built-in tools:
1. "Shell": For executing arbitrary commands in /bin/sh.
2. "EvalTaigo": For executing Go code using the internal Taigo VM.
3. "Taido": For delegating a specific sub-goal to a new autonomous agent.
These are the primary mechanisms for environment interaction and logic execution.

# 4.2 Completion Signal
The agent is instructed via the system prompt to conclude by calling the "Stop" tool once the primary objective is met. This tool requires a "reason" argument to summarize the outcome. While the system also monitors for text-based completion signals like "Goal achieved.", the "Stop" tool is the primary and mandatory mechanism for autonomous termination.

# 4.3 Output Management
To maintain focus and reduce cognitive noise during autonomous execution, the system suppresses the detailed logs of tool calls, results, and reasoning (thoughts) in the terminal output. Instead, it provides transient status indicators (e.g., "Executing Shell...") during tool execution, which are cleared upon completion. Only final results remain visible to provide context without overwhelming the user with mechanical details.

# 4.4 State Preparation
To ensure predictable output behavior and prevent duplicated tool handlers, the autonomous execution logic unwraps any existing "Output" or "FuncMap" wrappers from the incoming state. It then applies its own specialized wrappers tailored for non-interactive execution. This guarantees conceptual integrity of the interaction turn.

# 5. Success Metrics
Success is defined by the autonomous transition from an initial state to a verified goal state with zero manual steps during execution.

# 6. Security and Sandboxing
# 6.1 Principle of Least Privilege
To prevent the autonomous agent from causing unintended damage to the host system, Taido implements a best-effort filesystem sandbox using Linux Landlock. If the kernel supports Landlock, it reduces the blast radius of any tool-calling errors. If Landlock is unavailable, the system proceeds with a warning, emphasizing the importance of running Taido in a controlled environment.

# 6.2 Write Restriction
When sandboxing is active, the agent is strictly restricted to writing only within the current working directory. This ensures that any files created, modified, or deleted by tools (including those run via the "Shell" tool) are contained within the project scope.

# 6.3 Unrestricted Reading
The agent retains unrestricted read access to the entire filesystem (where permissions allow). This is necessary for the agent to gather context, such as reading system headers, library source code, or configuration files located outside the working directory, which informs its reasoning process.

# 6.4 Subprocess Inheritance
The Landlock ruleset and the "no new privileges" flag are inherited by all subprocesses. Consequently, the "Shell" tool and any commands it executes are subject to the same security constraints as the primary Taido process.

# 7. Testing Strategy
# 7.1 Sandbox Verification
Testing process-level security features like Landlock requires subprocess isolation. Unit tests for the sandbox spawn a separate instance of the test binary to apply the sandbox and attempt restricted operations. This prevents the primary test runner from being constrained and allows for precise verification of "Permission Denied" errors when attempting to write outside the authorized scope.
`)

const ObsoleteTheory = (`
# 1. Obsolete Completion Signal (prior to reason parameter)
# 4.2 Completion Signal
The agent is instructed via the system prompt to conclude by calling the "Stop" tool once the primary objective is met. Alternatively, stating "Goal achieved." also serves as a terminal condition. This provides a robust mechanism for the autonomous loop to exit.

# 2. Obsolete Safety Limit
The initial implementation included a "maxIterations" safety limit (e.g., 50 steps) to prevent infinite loops. This was removed to accommodate complex, long-running tasks, shifting the responsibility for termination to the model and termination detection logic.

# 3. Absolute Sandboxing Requirement
# 6.1 Principle of Least Privilege
To prevent the autonomous agent from causing unintended damage to the host system, Taido implements a filesystem sandbox using Linux Landlock. This reduces the blast radius of any tool-calling errors.
(Obsolete because it caused failures in environments without Landlock support, e.g., CI or older kernels. Sandboxing is now best-effort.)
`)