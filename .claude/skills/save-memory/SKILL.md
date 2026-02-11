---
name: save-memory
description: Save memories to semantic memory store during execution. NOT user-invokable.
user-invocable: false
---

# Save Memory

Save important information to the semantic memory store at any point during execution. Memories are automatically embedded for future search via the memory-lookup skill.

## Usage

```bash
uv run --project /path/to/jarvis python -c "
from jarvis.memory import MemoryManager
m = MemoryManager('data')
m.save('the memory content here')
"
```

## What to save

- Preferences the user mentions (food, music, workflow, tools)
- Project updates and decisions
- Important dates, people, relationships
- Things the user explicitly asks you to remember
- Context that would be useful in future conversations

## Notes

- Save memories whenever something worth remembering comes up — don't wait until the end
- Keep memory content concise and factual
- One memory per distinct piece of information (don't bundle unrelated things)
- Memories are searchable via the memory-lookup skill
