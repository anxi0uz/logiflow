# Logiflow

Persistent project context is stored in `docs/ai/`.

At the start of a substantial task:
- read `docs/ai/current-state.md`
- read the other `docs/ai/` files relevant to the task
- treat repository code, migrations and API schema as authoritative

After substantial changes:
- update affected `docs/ai/` files
- replace stale information instead of appending task history
- keep documentation concise
- record durable non-obvious decisions in `decisions.md`

Do not rely on previous chat history as project documentation.
