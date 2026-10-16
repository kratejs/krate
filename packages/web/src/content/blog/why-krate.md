---
title: Why a Go-native web framework
description: Whole-program compilation, signals, and zero Node at build time.
date: 2026-03-01
author: Krate
tags: [compiler, go, signals]
draft: false
order: 1
---

Most JS meta-frameworks compile your app with a JavaScript toolchain running on
Node. Krate starts from a different premise: your app is *data* that a Go binary
can lex, parse, analyze, and render into static HTML.

## Whole-program by construction

Because the compiler sees the whole program, it can fold data fetching at build
time, inline components, and emit only the hydration code a page actually needs.
There is no separate bundler to configure and no Node process to supervise.

## Signals over virtual DOM

Interactivity is expressed as signals. The runtime binds them directly to DOM
nodes, so updates are surgical and the client payload stays small.
