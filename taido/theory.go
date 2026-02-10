package taido

const Theory = `
# 1. Core Mission
Taido is a minimalist autonomous execution engine designed to automate repetitive, mechanical cognitive tasks. It is built for scenarios where the process is structured but requires a degree of LLM-driven decision-making to bridge tool outputs. A typical use case is the iterative optimization of a trading strategy, where the agent executes backtests, analyzes results, and adjusts parameters without human oversight.

# 2. Design Philosophy
# 2.1 Autonomous Execution
The system operates on a "set and forget" model. Once a goal is defined via an initial prompt, Taido executes the full lifecycle of reasoning and tool usage without human intervention. This eliminates the latency and cognitive load of interactive interfaces.

# 2.2 Minimalist Architecture
Taido eschews complex frameworks and multi-agent abstractions. It provides a lean ReAct (Thought-Action-Observation) loop. It is a coordinator, not a monolith; it relies on external tools for specialized logic, data processing, and heavy computation.

# 2.3 Functional Efficiency
The focus is on "doing" rather than "chatting." The tool is optimized for high-leverage actions where a small amount of reasoning can trigger significant external processes.

# 3. Strategic Constraints
# 3.1 Non-Interactivity
There is no provision for mid-process user input. All necessary information, constraints, and permissions must be established in the initial prompt or defined within the tool environment.

# 3.2 Tool-Centric Problem Solving
If a problem is complex, Taido should not be taught to solve it internally. Instead, an external tool or program should be developed to solve it, and Taido should be instructed on how to invoke and interpret that tool.

# 4. Implementation Details
# 4.1 ReAct Loop
The core execution is a ReAct loop: Generate -> Execute Tools -> Observe -> Repeat. The loop continues until the agent explicitly signals completion (e.g., via "Goal achieved.") or no tool calls are generated in a model response.

# 4.2 Completion Signal
The agent is instructed via the system prompt to conclude with "Goal achieved." once the primary objective is met. This serves as the terminal condition for the autonomous loop.

# 5. Success Metrics
Success is defined by the autonomous transition from an initial state to a verified goal state with zero manual steps during execution.
`