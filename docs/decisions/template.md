# NNNN. Short statement of the decision, in the present tense

**Status:** Accepted | Superseded by [NNNN](./NNNN-slug.md)

## Context

The forces. What made a decision necessary at all: the constraint, the failure,
the requirement. Written so that someone who was not there can feel the pressure
that produced the choice.

State facts, not conclusions. If a measurement or an observation drove this, put
the number here.

## Decision

What was chosen. One paragraph, in the present tense, active voice: "The COM layer
is written in this repository." Not "we decided that we should probably write".

## Alternatives rejected

Each one a reasonable engineer would have picked, and why it was not. Be fair to
them - a strawman here is a decision that will be reopened, because the next
person will notice the strawman and correctly stop trusting the record.

If an alternative was actually tried and failed, do not repeat the story: link to
[lessons-and-dead-ends.md](../lessons-and-dead-ends.md), which is where failures
live.

## Consequences

What this costs, permanently. Every real decision has a price; a record that lists
only benefits is advertising, not engineering.

Include the constraint it imposes on future work. If the choice means that
something can never be done, or must always be done, say so plainly - that is the
sentence a future agent most needs to read before proposing the opposite.

## What would change our mind

The trip-wire. The observation that would make this decision wrong:

- a specific thing an upstream project could do;
- a measurement that would contradict the assumption it rests on;
- a requirement that would make the cost above unacceptable.

If you cannot name one, the decision is either trivial (do not write a record) or
you have not understood it yet (do not write it *yet*).

## Evidence

The commit, the test, the log line, the live observation. Not a summary of the
code, and not a generated map of the repository: those are descriptions, not
observations.
