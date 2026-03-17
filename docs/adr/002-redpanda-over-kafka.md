# ADR-002: Redpanda over Apache Kafka

## Status
Accepted

## Context
The system needs a message broker for asynchronous communication between the Go API Gateway and the Python Worker. The broker must support the Kafka protocol (widely adopted, mature client libraries in Go and Python) while being lightweight enough for local development.

## Decision
Use Redpanda as the Kafka-compatible message broker.

Redpanda is written in C++ and implements the Kafka protocol natively. It requires no JVM and no Zookeeper/KRaft controller, running as a single binary.

## Alternatives Considered
- **Apache Kafka:** The industry standard, but requires JVM (~2GB RAM), Zookeeper or KRaft for coordination, and multiple processes. Overly heavy for a development environment with a single broker.
- **NATS JetStream:** Lightweight and Go-native, but uses a proprietary protocol. Would require custom client wrappers instead of using standard Kafka libraries.
- **RabbitMQ:** Excellent for task queues, but less suitable for event streaming patterns (topic-based, partitioned, replayable logs).

## Consequences
**Positive:**
- ~512MB RAM vs ~2GB for Kafka — significantly lighter for development
- Same Kafka protocol means identical client libraries (segmentio/kafka-go in Go, confluent-kafka in Python)
- Redpanda Console provides a free built-in UI for topic inspection — no need for separate Kafka tools
- Single container, no Zookeeper/KRaft coordination needed
- Production-grade: used by companies like Cisco and Alpaca

**Negative:**
- Smaller community than Apache Kafka
- Some advanced Kafka features (Kafka Streams, Connect) are not natively supported
- For production at extreme scale, Apache Kafka has more battle-testing
