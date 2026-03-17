# ADR-007: Fiber over Gin/Chi

## Status
Accepted

## Context
The Go API Gateway needs an HTTP framework that provides routing, middleware support, and high performance. The framework choice impacts development velocity, performance characteristics, and the available middleware ecosystem.

## Decision
Use Fiber (gofiber/fiber) as the HTTP framework.

Fiber is built on top of fasthttp (not net/http), providing significantly higher throughput for I/O-bound workloads. Its API is inspired by Express.js, making it familiar to developers coming from the Node.js ecosystem.

## Alternatives Considered
- **Gin:** The most popular Go web framework. Built on net/http, excellent middleware ecosystem, large community. However, slightly lower raw performance than Fiber in benchmarks, and the API is less intuitive for developers familiar with Express-style routing.
- **Chi:** Minimalist router built on net/http. Lightweight and composable, but lacks built-in features like body parsing, file upload handling, and structured error responses that Fiber provides out of the box.
- **Standard library (net/http):** Maximum control, zero dependencies. But requires significant boilerplate for routing, middleware chaining, body parsing, and response helpers. Not practical for rapid development.

## Consequences
**Positive:**
- Highest raw throughput among Go web frameworks (fasthttp-based)
- Express-like API reduces development time (familiar patterns)
- Built-in multipart file upload handling (critical for PDF upload endpoint)
- Rich middleware ecosystem (CORS, recover, rate limiting)
- Active development and community

**Negative:**
- fasthttp is not fully net/http compatible — some standard library middleware won't work
- Smaller ecosystem than Gin (fewer third-party middleware packages)
- fasthttp's memory pooling can cause subtle bugs if response/request objects are used after handler returns
- Less battle-tested than Gin in production environments
