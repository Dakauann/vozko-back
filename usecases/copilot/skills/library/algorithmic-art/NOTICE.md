# Notice

This skill is a modified version of the `algorithmic-art` skill from https://github.com/anthropics/skills, Copyright 2026 Anthropic, PBC, licensed under the Apache License, Version 2.0 (see LICENSE.txt in this folder). The text is kept in the original English and wording; only the changes below were made, so it works with the Vozko Estúdio editor, Elo's tools and the skill validator.

Changes made by Vozko on 2026-10-09:
- Description, introduction and output: p5.js code, the HTML viewer and .md/.html/.js files became an algorithm you compute and build on an artboard of the Estúdio image editor, with the philosophy written in the reply.
- "P5.JS IMPLEMENTATION" (template step, code samples, canvas setup) became "ESTÚDIO IMPLEMENTATION": how to compute elements from the seed and build them with the editor operations in batches; the seeded randomness and parameter guidance were kept as principles.
- "INTERACTIVE ARTIFACT CREATION", the HTML artifact structure, the sidebar, the Anthropic branding constants and "RESOURCES" (templates) were removed: the editor has no code runtime or viewer.
- Variations: seed navigation buttons became more artboards built with other seeds and named after them.
- "Seeded Randomness (Art Blocks Pattern)" became "Seeded Randomness", so the skill names no platform.
- "Performance" became a workable element count; "Don't copy the flow field example" became "Don't default to a flow field" and the reminder to keep the template UI and Anthropic branding was removed (the example template is not shipped).
- "The next Claude" changed to "the next pass", because Elo may run on other models.

Only `SKILL.md` is loaded by the product. This notice and the license stay with the source.
