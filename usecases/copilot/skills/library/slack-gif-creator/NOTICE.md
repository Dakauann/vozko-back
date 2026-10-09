# Notice

This skill is a modified version of the `slack-gif-creator` skill from https://github.com/anthropics/skills, Copyright 2026 Anthropic, PBC, licensed under the Apache License, Version 2.0 (see LICENSE.txt in this folder). The text is kept in the original English and wording; only the changes below were made, so it works with the Vozko Estúdio editor, Elo's tools and the skill validator.

Changes made by Vozko on 2026-10-09:
- Description and introduction: "animated GIFs optimized for Slack" became short looping animations made as Estúdio video projects, the kind shared as GIFs.
- Slack requirements: the FPS and color-count parameters (GIF encoder settings) were removed; a note says the Estúdio exports MP4 at 1080x1080 and that a GIF or a 128x128 emoji needs an outside converter.
- "Core Workflow" (Python GIFBuilder code) became the editor workflow: square video project, elements, keyframes, matching first and last keys, MP4 export.
- Drawing with PIL ImageDraw became the editor's shapes, vector paths, presets and icons; uploads use add_media.
- "Available Utilities" (GIFBuilder, validators, easing module, frame helpers) became "Available Tools" (shapes, the keyframe easings, motion presets).
- Each animation concept keeps its idea; the Python and PIL calls became keyframe instructions (math.sin and velocity updates became alternating keys and easings).
- "Optimization Strategies" for GIF size became a short "File Size" note; "Dependencies" (pip install) was removed.

Only `SKILL.md` is loaded by the product. This notice and the license stay with the source.
