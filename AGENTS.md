# Agents

Don't commit or push to git, I will do that on my own.

Never pass a bare string as the arguments to a runtime builtin tool (e.g. `tools.process.start('...')`). Always use the documented object shape — `tools.process.start({ command: '...', cwd: '...', env: { ... } })` — otherwise the session dies with a pydantic `ValidationError` on `call.runtime_builtin.arguments` ("Input should be an object"). Prefer `cwd:` over `cd ... &&` and `env:` over `VAR=x ...` prefixes; use the `bash` tool for short one-off commands.
