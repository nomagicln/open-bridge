---
layout: home

hero:
  name: "OpenBridge"
  text: "One Spec, Dual Interface."
  tagline: A universal bridge connecting AI agents with everything.
  image:
    src: /logo.jpeg
    alt: OpenBridge Logo
  actions:
    - theme: brand
      text: Get Started
      link: /guide/quick-start
    - theme: alt
      text: View on GitHub
      link: https://github.com/nomagicln/open-bridge

features:
  - title: No SDK Code Generation
    details: Behavior is driven dynamically by OpenAPI specifications. No generated clients to maintain.
  - title: Semantic CLI
    details: Resource-oriented commands (e.g., `myapp user create`) that make sense to humans.
  - title: AI Agent Native
    details: Built-in Model Context Protocol (MCP) server support for seamless integration with AI agents like Claude.
  - title: Progressive Disclosure
    details: Intelligent tool discovery strategy to handle large APIs without overwhelming AI context.
  - title: Secure & Fast
    details: Uses the system keyring (with an encrypted file-backend fallback), caches remote OpenAPI specs for offline reuse, supports per-profile TLS/retry policies, and starts in single-digit milliseconds.
  - title: Multi-Platform
    details: Works natively on Linux, macOS, and Windows.
---
