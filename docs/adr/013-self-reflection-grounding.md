# ADR-013: Self-Reflection / LLM-as-a-Judge for Grounding

## Status
Accepted

## Context
RAG systems can hallucinate even with retrieved context: the LLM may add facts not present in the chunks, infer unsupported conclusions, or fabricate details. For an enterprise system handling sensitive documents, ungrounded answers are unacceptable — they erode trust and can lead to incorrect decisions.

## Decision
Implement a **Self-Reflection** pattern (also known as LLM-as-a-Judge) that evaluates every generated answer before returning it to the user:

1. **Draft step:** Generate the answer from retrieved chunks (existing behavior).
2. **Evaluation step:** Send the question, context, and draft to a second LLM call with a strict evaluator prompt. The evaluator returns `{"is_grounded": true/false, "reason": "..."}`.
3. **Retry loop (max 2 attempts):** If the evaluator says `is_grounded: false`, the system rewrites the answer with a stricter prompt (including the rejection reason) and re-evaluates.
4. **Fallback:** If still not grounded after retries, return a safe fallback message: "Desculpe, mas nao encontrei informacao suficiente na base de conhecimento para garantir uma resposta precisa."
5. **Cache policy:** Only grounded answers are cached, preventing hallucinated responses from being served to future users.

The evaluation prompt enforces strict criteria: every claim must be directly verifiable from the provided context, with no fabricated information or unsupported inferences.

## Alternatives Considered
- **NLI (Natural Language Inference) models:** Faster but less accurate for complex grounding checks; requires an additional model deployment.
- **No evaluation (trust the generation prompt):** The generation prompt already says "use ONLY the provided excerpts," but LLMs don't always comply. Post-hoc verification is more reliable.
- **Human-in-the-loop:** Not feasible for real-time queries; self-reflection is the automated equivalent.

## Consequences
**Positive:**
- Dramatically reduces hallucinations reaching users
- Fail-safe: ungrounded answers are replaced with an honest fallback
- Only grounded answers are cached, maintaining cache quality
- `grounded` field in the API response provides transparency

**Negative:**
- Adds 1-3 extra LLM calls per query (evaluation + optional rewrite + re-evaluation)
- Increases latency by `eval_ms` (visible in timing breakdown)
- Increases API costs (mitigated by router selecting cheaper models for simple queries)
- Evaluator itself could have false negatives (rejecting correct answers)
