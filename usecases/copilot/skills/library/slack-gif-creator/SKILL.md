---
name: slack-gif-creator
description: Knowledge for creating short looping animations in an Estúdio video project, the kind shared as GIFs in Slack, WhatsApp or social posts. Provides constraints and animation concepts. Use when users request an animated GIF, loop, sticker or reaction like "make me a GIF of X doing Y".
license: Apache 2.0, complete terms in LICENSE.txt. Modified by Vozko for the Estúdio editor and Elo's tools; every change is listed in NOTICE.md.
---

# Slack GIF Creator

Knowledge for creating short looping animations, made as Estúdio video projects, like the animated GIFs shared in Slack.

## Slack Requirements

**Dimensions:**
- Emoji GIFs: 128x128 (recommended)
- Message GIFs: 480x480

**Parameters:**
- Duration: Keep under 3 seconds for emoji GIFs

**In the Estúdio:** projects export as MP4 video, not as GIF files, and the square format renders at 1080x1080. Make the loop in a square video project; tell the user it exports as MP4, which Slack and WhatsApp play as a loop, and that a GIF or an emoji of 128x128 needs a converter outside the Estúdio.

## Core Workflow

1. Create a square video project (set_canvas aspect square) and keep the loop short.
2. Add the elements with add_shape, add_icon, add_text or add_media, all starting at at_ms 0 and lasting the whole loop.
3. Animate them with animate (keyframes with easings) or motion_preset.
4. Make it loop: the last key of every property equals its first key.
5. The user exports it from the editor as an MP4 video.

## Drawing Graphics

### Working with User-Uploaded Images
If a user uploads an image, consider whether they want to:
- **Use it directly** (e.g., "animate this", "split this into frames")
- **Use it as inspiration** (e.g., "make something like this")

Put an uploaded image in the project with add_media (the media_id of the attachment).

### Drawing from Scratch
When drawing graphics from scratch, use the editor's primitives:

- Circles and ovals: add_shape with shape ellipse, with fill and stroke
- Stars, triangles, any polygon: shape star or triangle, or shape path with a vector path
- Lines: shape line or arrow
- Rectangles: shape rect (radius rounds the corners)
- Ready vector forms: path_preset (heart, burst, seal, swoosh, wave, blob and others)

**Don't use:** Emoji fonts (unreliable across platforms) or assume pre-packaged graphics exist in this skill. The icon catalog (add_icon) is available.

### Making Graphics Look Good

Graphics should look polished and creative, not basic. Here's how:

**Use thicker lines** - Always set stroke_width high enough to read at the final size (at 1080 px, 12 or more). Thin lines look choppy and amateurish.

**Add visual depth**:
- Use gradients for backgrounds (a full-frame rect with a gradient)
- Layer multiple shapes for complexity (e.g., a star with a smaller star inside)

**Make shapes more interesting**:
- Don't just draw a plain circle - add highlights, rings, or patterns
- Stars can have glows (draw larger, semi-transparent versions behind)
- Combine multiple shapes (stars + sparkles, circles + rings)

**Pay attention to colors**:
- Use vibrant, complementary colors
- Add contrast (dark outlines on light shapes, light outlines on dark shapes)
- Consider the overall composition

**For complex shapes** (hearts, snowflakes, etc.):
- Use combinations of polygons and ellipses
- Calculate points carefully for symmetry
- Add details (a heart can have a highlight curve, snowflakes have intricate branches)

Be creative and detailed! A good Slack GIF should look polished, not like placeholder graphics.

## Available Tools

### Shapes, icons and text
add_shape, add_icon and add_text with fill, gradient, stroke, shadow and blend_mode.

### Easing
Smooth motion instead of linear: every key of animate takes an easing for the segment that leaves it.
Available: linear, easeIn, easeOut, easeInOut, bounce (bounce out), elastic (elastic out), backOut (back out), plus backIn, backInOut, spring, hold and cubic-bezier(x1,y1,x2,y2).

### Ready animations
motion_preset (pulse, spin, wobble, pop, drop, springIn and others), entrance and exit.

## Animation Concepts

### Shake/Vibrate
Offset object position with oscillation:
- Alternate keys on x and/or y around the position, every 2 to 3 frames
- Add small random variations for natural feel
- Apply to x and/or y position

### Pulse/Heartbeat
Scale object size rhythmically:
- Alternate scale keys with easeInOut for a smooth pulse (or motion_preset pulse)
- For heartbeat: two quick pulses then pause
- Scale between 0.8 and 1.2 of base size

### Bounce
Object falls and bounces:
- Animate y with easing bounce for landing (or motion_preset drop)
- Use easeIn for falling (accelerating)
- That easeIn is the gravity

### Spin/Rotate
Rotate object around center:
- Animate rotation with linear keys (or motion_preset spin)
- For wobble: alternate rotation keys instead of linear (or motion_preset wobble)

### Fade In/Out
Gradually appear or disappear:
- Animate opacity
- Or crossfade two elements with opposite opacity keys
- Fade in: alpha from 0 to 1
- Fade out: alpha from 1 to 0

### Slide
Move object from off-screen to position:
- Start position: outside frame bounds
- End position: target location
- Use easing easeOut for smooth stop
- For overshoot: use easing backOut

### Zoom
Scale and position for zoom effect:
- Zoom in: scale from 0.1 to 2.0
- Zoom out: scale from 2.0 to 1.0
- Can add blur keys for drama

### Explode/Particle Burst
Create particles radiating outward:
- Create many small shapes at one point, each with its own angle and distance
- Animate each one outward with x and y keys (easeOut)
- Add gravity: a last y segment with easeIn
- Fade out particles over time (opacity keys)

## File Size

The Estúdio export sets the encoding. Only when asked to make the file smaller, make the loop shorter.

## Philosophy

This skill provides:
- **Knowledge**: Slack's requirements and animation concepts
- **Tools**: the editor's shapes, keyframes and easings
- **Flexibility**: Create the animation logic with the editor's primitives

It does NOT provide:
- Rigid animation templates or pre-made functions
- Emoji font rendering (unreliable across platforms)
- A library of pre-packaged graphics built into the skill

**Note on user uploads**: This skill doesn't include pre-built graphics, but if a user uploads an image, add it with add_media and work with it - interpret based on their request whether they want it used directly or just as inspiration.

Be creative! Combine concepts (bouncing + rotating, pulsing + sliding, etc.) and use the editor's full capabilities.
