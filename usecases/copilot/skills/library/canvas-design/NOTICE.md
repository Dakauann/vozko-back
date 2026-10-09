# Notice

This skill is a modified version of the `canvas-design` skill from https://github.com/anthropics/skills, Copyright 2026 Anthropic, PBC, licensed under the Apache License, Version 2.0 (see LICENSE.txt in this folder). The text is kept in the original English and wording; only the changes below were made, so it works with the Vozko Estúdio editor, Elo's tools and the skill validator.

Changes made by Vozko on 2026-10-09:
- Description and steps: .png, .pdf and .md file outputs became an artboard of the Estúdio image editor and the philosophy written in the reply; pages became artboards.
- Font sources: the ./canvas-fonts directory and font downloads became the Estúdio font catalog (font_id).
- "Double-check" now names studio_look; "call a new function" became "add a new layer"; "go back to the code" became "go back to the artboard".
- "The next Claude" changed to "the next pass", because Elo may run on other models.
- Long dashes replaced with plain punctuation (the skill validator rejects them).

Only `SKILL.md` is loaded by the product. This notice and the license stay with the source.
