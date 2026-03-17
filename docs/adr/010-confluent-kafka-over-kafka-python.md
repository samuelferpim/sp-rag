# ADR-010: confluent-kafka over kafka-python

## Status
Accepted

## Context
The Python Worker needs a Kafka consumer to listen for `document.uploaded` events. Two main Python Kafka libraries exist:
1. `confluent-kafka` — a Python wrapper around librdkafka (C library by Confluent)
2. `kafka-python` — a pure Python implementation of the Kafka protocol

## Decision
Use `confluent-kafka` as the Kafka client library for the Python Worker.

confluent-kafka wraps librdkafka, a high-performance C library that implements the Kafka protocol. It provides significantly better throughput, lower latency, and more reliable offset management compared to the pure Python alternative.

## Alternatives Considered
- **kafka-python:** Pure Python implementation. Easier to install (no C dependencies), but approximately 10x slower in throughput benchmarks. The library has had periods of inactivity in maintenance, and its API is less type-safe. Not recommended for production workloads.
- **aiokafka:** Async Python Kafka client built on kafka-python. Inherits the performance limitations of kafka-python. Useful for async frameworks (FastAPI), but our worker is a synchronous consumer loop — async adds unnecessary complexity.
- **faust:** Stream processing library by Robinhood. Feature-rich but heavy, designed for stream processing applications rather than simple consumer patterns. Overkill for our ETL pipeline.

## Consequences
**Positive:**
- ~10x better throughput than kafka-python (C-based processing)
- Better type annotations and API design
- Active maintenance by Confluent (the company behind Kafka)
- Reliable offset management and consumer group coordination
- Lower CPU usage per message processed

**Negative:**
- Requires C compiler and librdkafka for installation (handled in Dockerfile with system packages)
- Slightly more complex installation in development environments
- Larger Docker image due to C dependencies (~50MB additional)
- API style differs from kafka-python (callbacks vs iterators)
