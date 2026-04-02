# Vibe Agents 🤖

A multi-agent AI "vibe coding" system that generates complete, production-ready software systems from high-level natural language prompts.

## Architecture

```
🎯 Orchestrator
     ├── 🏗  Architect  → Designs system blueprint (tech stack, DB schema, API endpoints)
     ├── 📐 Planner    → Creates ordered task graph from the blueprint
     ├── 🧩 Builder    → Generates complete code files for each task
     ├── 🔍 Reviewer   → Validates code quality & security (with retry loop)
     ├── 🧪 Tester     → Generates comprehensive test suites
     └── 🔄 Iterator   → Applies user feedback with minimal regeneration
```

### Agent Pipeline

```
User Prompt
    │
    ▼
Orchestrator ──→ Architect ──→ Planner ──→ Builder ──→ Reviewer ──┐
                                                                    │
                                              ◄─── rebuild if ─────┘
                                              critical issues
                                                    │
                                                    ▼
                                                 Tester
                                                    │
                                                    ▼
                                              Output Files
                                                    │
                                              User Feedback
                                                    │
                                                    ▼
                                               Iterator ──→ Reviewer
```

### Reasoning Strategies

| Strategy | Agents | Description |
|----------|--------|-------------|
| **CoT** (Chain-of-Thought) | Architect, Planner, Builder, Tester | Step-by-step linear reasoning |
| **ToT** (Tree-of-Thought) | Reviewer | Explores multiple analysis paths |
| **ReAct** (Reason + Act) | Orchestrator, Iterator | Iterative reasoning with actions |

## Quick Start

### Prerequisites

- Go 1.21+
- A supported LLM provider (GitHub Copilot recommended)

### Installation

```bash
# Clone the repository
git clone https://github.com/dhawalhost/vibe-agents
cd vibe-agents

# Build the binary
make build

# Or install globally
make install
```

### Configuration

Set your provider credentials as environment variables:

```bash
# GitHub Copilot (default)
export GITHUB_TOKEN=your_github_token

# OpenAI
export OPENAI_API_KEY=your_openai_key

# Anthropic
export ANTHROPIC_API_KEY=your_anthropic_key
```

### Generate a System

```bash
# Basic usage
./bin/vibe generate "Build a REST API with user authentication and PostgreSQL"

# Specify output directory
./bin/vibe generate "Build a React dashboard with real-time data" --output ./my-dashboard

# Use a specific model
./bin/vibe generate "SaaS app with billing" --model gpt-4o --output ./my-saas

# Use a different provider
./bin/vibe generate "Microservices architecture" --provider openai --output ./microservices
```

### Iterate on Generated Code

```bash
# Apply feedback to an existing project
./bin/vibe iterate "Add dark mode support" --project ./my-dashboard

# Switch database
./bin/vibe iterate "Switch from PostgreSQL to MongoDB" --project ./my-api

# Add a feature
./bin/vibe iterate "Add rate limiting to all API endpoints" --project ./my-api
```

### Inspect Chain of Thought

```bash
# See how the agents reasoned about your project
./bin/vibe explain --project ./my-api
```

### Manage Configuration

```bash
# Set default provider
./bin/vibe config set provider copilot

# Set default model
./bin/vibe config set model gpt-4o
```

## Supported LLM Providers

| Provider | Models | Auth |
|----------|--------|------|
| **GitHub Copilot** (default) | gpt-4o, gpt-4o-mini, claude-sonnet-4, o1-preview | `GITHUB_TOKEN` |
| **OpenAI** | gpt-4o, gpt-4o-mini, gpt-4-turbo | `OPENAI_API_KEY` |
| **Anthropic** | claude-opus-4-5, claude-sonnet-4-5, claude-haiku-4-5 | `ANTHROPIC_API_KEY` |
| **Ollama** (local) | llama3, mistral, codellama | None (local) |

## Configuration File

The system looks for configuration in these locations (in order):
1. `~/.vibe-agents/config.yaml`
2. `./configs/default.yaml`
3. Environment variables (`VIBE_PROVIDER`, `VIBE_MODEL`)

```yaml
provider: copilot
model: gpt-4o

agents:
  architect:
    model: gpt-4o
    reasoning: cot
  reviewer:
    model: gpt-4o
    reasoning: tot   # Tree-of-Thought for deeper analysis

output:
  directory: ./output
  overwrite: false

logging:
  level: info
  chain_of_thought: true
```

## Project Structure

```
vibe-agents/
├── cmd/vibe/           # CLI entry point
├── internal/
│   ├── agents/         # Specialized AI agents
│   ├── context/        # Shared state management
│   ├── llm/            # LLM provider implementations
│   ├── reasoning/      # CoT, ToT, ReAct strategies
│   ├── prompt/         # Dynamic prompt construction
│   ├── output/         # File I/O and reporting
│   └── config/         # Configuration management
├── pkg/types/          # Shared type definitions
└── configs/            # Default configuration
```

## Development

```bash
# Run tests
make test

# Run tests with coverage
make test-coverage

# Build
make build

# Format code
make fmt

# Run go vet
make vet
```

## How It Works

1. **Architect Agent** receives your prompt and designs a complete system blueprint (JSON) including tech stack, database schema, API endpoints, auth strategy, and deployment config.

2. **Planner Agent** converts the blueprint into an ordered dependency graph of implementation tasks. Each task specifies which files to create and includes a precise prompt for the Builder.

3. **Builder Agent** generates complete, production-ready code files for each task in dependency order. It maintains full context of previously generated files to ensure consistency.

4. **Reviewer Agent** performs multi-path analysis of the generated code using Tree-of-Thought reasoning. Critical issues trigger an automatic rebuild (up to 3 retries).

5. **Tester Agent** generates comprehensive test suites (unit, integration, edge cases) based on the generated source files.

6. **Iterator Agent** (on feedback) performs targeted analysis to identify the minimal set of files that need to change, avoiding full regeneration.

## Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Run tests (`make test`)
4. Commit your changes
5. Push and open a Pull Request

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
