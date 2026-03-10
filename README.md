# 🛡️ Secure Polyglot RAG (SP-RAG)

![Golang](https://img.shields.io/badge/Golang-1.21+-00ADD8?style=for-the-badge&logo=go)
![Python](https://img.shields.io/badge/Python-3.11+-3776AB?style=for-the-badge&logo=python)
![Kafka](https://img.shields.io/badge/Apache_Kafka-231F20?style=for-the-badge&logo=apache-kafka)
![Qdrant](https://img.shields.io/badge/Qdrant-Vector_DB-FF5252?style=for-the-badge)
![SpiceDB](https://img.shields.io/badge/SpiceDB-Zero_Trust-4285F4?style=for-the-badge)

An experimental, high-performance **Retrieval-Augmented Generation (RAG)** architecture designed for enterprise environments. 

SP-RAG solves three critical bottlenecks in modern Generative AI applications: **LLM latency, API costs, and data access governance**. By decoupling the heavy NLP ingestion pipeline (Python) from the high-concurrency API gateway and orchestration layer (Golang) via event-driven messaging, this system delivers secure, millisecond-level semantic searches.

### ✨ Key Features
* **Polyglot Microservices:** Python workers for data extraction (`unstructured.io`) and Golang API for blazingly fast request handling.
* **Zero-Trust AI (RBAC/ABAC):** Granular document-level access control using **SpiceDB** (Google Zanzibar model). The LLM only sees what the user is explicitly allowed to see.
* **Semantic Caching:** Drastically reduces OpenAI API costs and response times by caching similar queries in **Redis (RedisVL)**.
* **Event-Driven Ingestion:** Asynchronous document processing pipeline powered by **Apache Kafka**.
