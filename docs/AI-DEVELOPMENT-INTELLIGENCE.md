# AI Development Intelligence

## Objective

Measure how AI-assisted and agentic development changes engineering outcomes.

## Development modes

- HUMAN
- AI_ASSISTED
- AI_GENERATED
- AGENTIC
- AUTONOMOUS

Classification must be evidence-based where possible.

## AI dimensions

### Adoption

- AI-assisted PR percentage
- AI interaction volume
- agent run volume
- AI-enabled repositories

### Flow

- cycle time
- review latency
- throughput
- deployment frequency

### Quality

- defects
- CI failures
- rework
- security findings
- rollback/change failure

### Agent effectiveness

- task completion rate
- intervention rate
- retry rate
- test pass rate
- review acceptance
- time to successful completion

### Economics

- token/API cost
- cost per completed task
- cost per PR
- cost per successful change

### Rework

Track:

```text
AI contribution
    ->
review
    ->
modification
    ->
rework
    ->
final outcome
```

Raw lines of AI-generated code must never be treated as a productivity metric by themselves.

## Core question

The product should answer:

> What changed in engineering outcomes as AI adoption and agentic development changed?

rather than:

> How much AI are developers using?

## Board view

The board should show:

- adoption
- flow impact
- quality impact
- reliability impact
- AI cost
- agent effectiveness
- evidence coverage

## Agent experience

Later versions should identify agent bottlenecks such as:

- insufficient repository context
- ambiguous tickets
- failing tests
- unavailable tools
- excessive human intervention
- review bottlenecks

## Guardrails

The product must not:

- infer employee worth from AI usage
- equate lines of code with productivity
- claim AI caused an outcome without evidence
- expose private AI prompts unnecessarily
- require invasive surveillance when aggregate telemetry is sufficient
