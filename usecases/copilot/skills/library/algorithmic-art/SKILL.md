---
name: algorithmic-art
description: Creating algorithmic art on an artboard of the Estúdio image editor with seeded randomness and parameter exploration, computing every element yourself. Use this when users request generative art, algorithmic art, flow fields, particle systems or patterns made by rules. Create original algorithmic art rather than copying existing artists' work to avoid copyright violations.
license: Apache 2.0, complete terms in LICENSE.txt. Modified by Vozko for the Estúdio editor and Elo's tools; every change is listed in NOTICE.md.
---

Algorithmic philosophies are computational aesthetic movements that are then expressed through an algorithm. Output the philosophy in your reply and the generative art on an artboard of the open image project.

This happens in two steps:
1. Algorithmic Philosophy Creation (in your reply)
2. Express by building the generative art in the Estúdio image editor

First, undertake this task:

## ALGORITHMIC PHILOSOPHY CREATION

To begin, create an ALGORITHMIC PHILOSOPHY (not static images or templates) that will be interpreted through:
- Computational processes, emergent behavior, mathematical beauty
- Seeded randomness, noise fields, organic systems
- Particles, flows, fields, forces
- Parametric variation and controlled chaos

### THE CRITICAL UNDERSTANDING
- What is received: Some subtle input or instructions by the user to take into account, but use as a foundation; it should not constrain creative freedom.
- What is created: An algorithmic philosophy/generative aesthetic movement.
- What happens next: The same version receives the philosophy and EXPRESSES IT AS AN ALGORITHM - creating compositions that are 90% algorithmic generation, 10% essential parameters.

Consider this approach:
- Write a manifesto for a generative art movement
- The next phase involves running the algorithm that brings it to life

The philosophy must emphasize: Algorithmic expression. Emergent behavior. Computational beauty. Seeded variation.

### HOW TO GENERATE AN ALGORITHMIC PHILOSOPHY

**Name the movement** (1-2 words): "Organic Turbulence" / "Quantum Harmonics" / "Emergent Stillness"

**Articulate the philosophy** (4-6 paragraphs - concise but complete):

To capture the ALGORITHMIC essence, express how this philosophy manifests through:
- Computational processes and mathematical relationships?
- Noise functions and randomness patterns?
- Particle behaviors and field dynamics?
- Temporal evolution and system states?
- Parametric variation and emergent complexity?

**CRITICAL GUIDELINES:**
- **Avoid redundancy**: Each algorithmic aspect should be mentioned once. Avoid repeating concepts about noise theory, particle dynamics, or mathematical principles unless adding new depth.
- **Emphasize craftsmanship REPEATEDLY**: The philosophy MUST stress multiple times that the final algorithm should appear as though it took countless hours to develop, was refined with care, and comes from someone at the absolute top of their field. This framing is essential - repeat phrases like "meticulously crafted algorithm," "the product of deep computational expertise," "painstaking optimization," "master-level implementation."
- **Leave creative space**: Be specific about the algorithmic direction, but concise enough that the next pass has room to make interpretive implementation choices at an extremely high level of craftsmanship.

The philosophy must guide the next pass to express ideas ALGORITHMICALLY, not through hand-placed images. Beauty lives in the process, not the final frame.

### PHILOSOPHY EXAMPLES

**"Organic Turbulence"**
Philosophy: Chaos constrained by natural law, order emerging from disorder.
Algorithmic expression: Flow fields driven by layered Perlin noise. Thousands of particles following vector forces, their trails accumulating into organic density maps. Multiple noise octaves create turbulent regions and calm zones. Color emerges from velocity and density - fast particles burn bright, slow ones fade to shadow. The algorithm runs until equilibrium - a meticulously tuned balance where every parameter was refined through countless iterations by a master of computational aesthetics.

**"Quantum Harmonics"**
Philosophy: Discrete entities exhibiting wave-like interference patterns.
Algorithmic expression: Particles initialized on a grid, each carrying a phase value that evolves through sine waves. When particles are near, their phases interfere - constructive interference creates bright nodes, destructive creates voids. Simple harmonic motion generates complex emergent mandalas. The result of painstaking frequency calibration where every ratio was carefully chosen to produce resonant beauty.

**"Recursive Whispers"**
Philosophy: Self-similarity across scales, infinite depth in finite space.
Algorithmic expression: Branching structures that subdivide recursively. Each branch slightly randomized but constrained by golden ratios. L-systems or recursive subdivision generate tree-like forms that feel both mathematical and organic. Subtle noise perturbations break perfect symmetry. Line weights diminish with each recursion level. Every branching angle the product of deep mathematical exploration.

**"Field Dynamics"**
Philosophy: Invisible forces made visible through their effects on matter.
Algorithmic expression: Vector fields constructed from mathematical functions or noise. Particles born at edges, flowing along field lines, dying when they reach equilibrium or boundaries. Multiple fields can attract, repel, or rotate particles. The visualization shows only the traces - ghost-like evidence of invisible forces. A computational dance meticulously choreographed through force balance.

**"Stochastic Crystallization"**
Philosophy: Random processes crystallizing into ordered structures.
Algorithmic expression: Randomized circle packing or Voronoi tessellation. Start with random points, let them evolve through relaxation algorithms. Cells push apart until equilibrium. Color based on cell size, neighbor count, or distance from center. The organic tiling that emerges feels both random and inevitable. Every seed produces unique crystalline beauty - the mark of a master-level generative algorithm.

*These are condensed examples. The actual algorithmic philosophy should be 4-6 substantial paragraphs.*

### ESSENTIAL PRINCIPLES
- **ALGORITHMIC PHILOSOPHY**: Creating a computational worldview to be expressed through an algorithm
- **PROCESS OVER PRODUCT**: Always emphasize that beauty emerges from the algorithm's execution - each run is unique
- **PARAMETRIC EXPRESSION**: Ideas communicate through mathematical relationships, forces, behaviors - not static composition
- **ARTISTIC FREEDOM**: The next pass interprets the philosophy algorithmically - provide creative implementation room
- **PURE GENERATIVE ART**: This is about making LIVING ALGORITHMS, not hand-placed images with randomness
- **EXPERT CRAFTSMANSHIP**: Repeatedly emphasize the final algorithm must feel meticulously crafted, refined through countless iterations, the product of deep expertise by someone at the absolute top of their field in computational aesthetics

**The algorithmic philosophy should be 4-6 paragraphs long.** Fill it with poetic computational philosophy that brings together the intended vision. Avoid repeating the same points. Output this algorithmic philosophy in your reply.

---

## DEDUCING THE CONCEPTUAL SEED

**CRITICAL STEP**: Before implementing the algorithm, identify the subtle conceptual thread from the original request.

**THE ESSENTIAL PRINCIPLE**:
The concept is a **subtle, niche reference embedded within the algorithm itself** - not always literal, always sophisticated. Someone familiar with the subject should feel it intuitively, while others simply experience a masterful generative composition. The algorithmic philosophy provides the computational language. The deduced concept provides the soul - the quiet conceptual DNA woven invisibly into parameters, behaviors, and emergence patterns.

This is **VERY IMPORTANT**: The reference must be so refined that it enhances the work's depth without announcing itself. Think like a jazz musician quoting another song through algorithmic harmony - only those who know will catch it, but everyone appreciates the generative beauty.

---

## ESTÚDIO IMPLEMENTATION

With the philosophy AND conceptual framework established, express it on an artboard of the Estúdio image editor. Pause to gather thoughts before proceeding. Use only the algorithmic philosophy created and the instructions below.

There is no code runtime: you are the algorithm. Compute every element from the seed and the parameters, then create them with studio_edit_image operations (add_shape with shapes, stars, path_preset or a vector `path`, add_icon, add_text, gradients, blend_mode). Work in batches of up to 40 operations; build one well crafted base element, multiply it with duplicate_layers and place the copies with update_layer. A composition of dozens to a few hundred elements is the working range. Group each system (group_layers) and check every batch with studio_look.

### TECHNICAL REQUIREMENTS

**Seeded Randomness**:
Choose a seed number and derive every variation from it with the same deterministic sequence, so the same seed ALWAYS rebuilds the same piece.

**Parameter Structure - FOLLOW THE PHILOSOPHY**:

To establish parameters that emerge naturally from the algorithmic philosophy, consider: "What qualities of this system can be adjusted?"
- Quantities (how many?)
- Scales (how big? how fast?)
- Probabilities (how likely?)
- Ratios (what proportions?)
- Angles (what direction?)
- Thresholds (when does behavior change?)
- Colors

**To design effective parameters, focus on the properties the system needs to be tunable rather than thinking in terms of "pattern types".**

**Core Algorithm - EXPRESS THE PHILOSOPHY**:

**CRITICAL**: The algorithmic philosophy should dictate what to build.

To express the philosophy, avoid thinking "which pattern should I use?" and instead think "how to express this philosophy through an algorithm?"

If the philosophy is about **organic emergence**, consider using:
- Elements that accumulate or grow over time
- Random processes constrained by natural rules
- Feedback loops and interactions

If the philosophy is about **mathematical beauty**, consider using:
- Geometric relationships and ratios
- Trigonometric functions and harmonics
- Precise calculations creating unexpected patterns

If the philosophy is about **controlled chaos**, consider using:
- Random variation within strict boundaries
- Bifurcation and phase transitions
- Order emerging from disorder

**The algorithm flows from the philosophy, not from a menu of options.**

To guide the implementation, let the conceptual essence inform creative and original choices. Build something that expresses the vision for this particular request.

**Canvas Setup**: Use the artboard of the open project, or add_artboard with the size the piece needs.

### CRAFTSMANSHIP REQUIREMENTS

**CRITICAL**: To achieve mastery, create algorithms that feel like they emerged through countless iterations by a master generative artist. Tune every parameter carefully. Ensure every pattern emerges with purpose. This is NOT random noise - this is CONTROLLED CHAOS refined through deep expertise.

- **Balance**: Complexity without visual noise, order without rigidity
- **Color Harmony**: Thoughtful palettes, not random RGB values
- **Composition**: Even in randomness, maintain visual hierarchy and flow
- **Performance**: A workable element count, built in batches of up to 40 operations
- **Reproducibility**: Same seed ALWAYS produces identical output

### OUTPUT FORMAT

Output:
1. **Algorithmic Philosophy** - In your reply, explaining the generative aesthetic
2. **The artboard** - The generative composition built in the editor

---

## VARIATIONS & EXPLORATION

When the user wants variations, build the same algorithm with other seeds on more artboards of the same project (duplicate_artboard, then rebuild), each named after its seed ("Seed 42", "Seed 127"), side by side so they can compare.

This is like creating a series of prints from the same plate - the algorithm is consistent, but each seed reveals different facets of its potential.

---

## THE CREATIVE PROCESS

**User request** → **Algorithmic philosophy** → **Implementation**

Each request is unique. The process involves:

1. **Interpret the user's intent** - What aesthetic is being sought?
2. **Create an algorithmic philosophy** (4-6 paragraphs) describing the computational approach
3. **Implement it in the editor** - Run the algorithm that expresses this philosophy
4. **Design appropriate parameters** - What should be tunable?
5. **Name the seed and the parameters** - So the piece can be rebuilt or varied

**The constants**:
- A named seed (always present)
- The artboard as the canvas

**Everything else is variable**:
- The algorithm itself
- The parameters
- The visual outcome

To achieve the best results, trust creativity and let the philosophy guide the implementation.

**Critical reminder**:
- The **algorithm is where to create** something unique
- Don't default to a flow field - build what the philosophy demands
