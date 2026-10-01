# Product Requirements Document

## 1. Product

**Engineering Intelligence**

A standalone engineering intelligence platform that creates a continuously updated knowledge model of software delivery and explains how humans, AI assistants, and autonomous agents affect engineering outcomes.

## 2. Problem

Engineering organizations operate across fragmented systems:

- Jira / Linear / Azure DevOps
- GitHub / GitLab / Bitbucket
- CI/CD
- SonarQube and security tools
- incident management
- AI coding assistants and agents

Current tools provide metrics, but organizations still struggle to answer:

- Why did delivery performance change?
- What is actually constraining engineering flow?
- Is AI adoption improving outcomes?
- Where is AI creating rework?
- Are agentic workflows reducing or increasing risk?
- Which engineering changes are supported by evidence?
- How do engineering outcomes connect across work, code, deployment and production?

## 3. Product vision

Create a living engineering knowledge system that can answer both:

> What happened?

and:

> Why does the available evidence suggest it happened?

Every material insight should be traceable to source observations.

## 4. Target users

### Primary

- CTO
- VP Engineering
- Engineering Director
- Head of Engineering
- Engineering Manager
- Engineering Operations / DevEx

### Secondary

- Platform Engineering
- Staff / Principal Engineers
- Product leadership
- Security and reliability leadership

## 5. MVP scope

### Sources

1. GitHub
2. Jira
3. One AI-development telemetry source

The AI source must expose enough information to distinguish AI-assisted and agentic activity without requiring invasive developer surveillance.

### Canonical entities

- Organization
- Team
- Engineer
- Product
- Service
- Repository
- Issue
- Epic
- Commit
- PullRequest
- Review
- Build
- Deployment
- Incident
- AIAgent
- AIInteraction
- MetricObservation

### Core relationships

- Engineer MEMBER_OF Team
- Engineer AUTHORED Commit
- Commit PART_OF PullRequest
- PullRequest IMPLEMENTS Issue
- PullRequest TARGETS Repository
- PullRequest REVIEWED_BY Engineer
- PullRequest REQUESTED_REVIEW Engineer
- PullRequest PRODUCED_BY AIInteraction
- AIInteraction USES AIAgent
- PullRequest HAS_BUILD Build
- Build PRODUCED Deployment
- Deployment AFFECTS Service
- Deployment CAUSED Incident
- Issue PART_OF Epic

## 6. MVP capabilities

### Data convergence

- incremental connector sync
- checkpointing
- source provenance
- deduplication
- identity resolution
- temporal normalization

### Engineering intelligence

- cycle time
- PR throughput
- review latency
- deployment frequency
- change failure rate
- rework
- incident linkage

### AI development intelligence

- AI adoption
- AI-assisted development
- AI-generated activity where evidence exists
- agentic development activity
- AI-related rework
- AI-related review behaviour
- AI activity cost where available

### Intelligence

The system must answer questions such as:

- Why did Team X cycle time change?
- Which factors correlate with increased rework?
- How did AI adoption change delivery outcomes?
- Where is agentic development creating review bottlenecks?
- Which repositories show increased AI activity and degraded quality?

Answers must expose supporting evidence and confidence/limitations.

## 7. Non-goals

MVP will not:

- rank individual developers
- create an individual productivity score
- replace Jira/GitHub
- replace CI/CD
- become an AI coding assistant
- implement an LLM training platform
- put engineering-specific logic inside AIKOQL
- attempt every connector
- claim causality from observational data

## 8. Product success

MVP success requires:

1. trustworthy cross-source identity resolution
2. reproducible metrics
3. source-level provenance
4. meaningful AI-development attribution
5. evidence-backed explanations
6. useful answers to executive and engineering-manager questions
7. acceptable ingestion latency and reliability
8. AIKOQL remaining domain-agnostic

## 9. Product differentiator

The product is not another DORA dashboard.

The intended differentiation is:

**Engineering Knowledge + temporal reasoning + AI/agentic development intelligence + evidence-backed explanations.**
