---
title: Hydration without React
description: How Krate binds signals to the DOM with a tiny client runtime.
date: 2026-02-10
author: Krate
tags: [runtime, hydration, signals]
draft: false
order: 2
---

Krate does not ship a virtual DOM. Instead, the compiler emits hydration
markers that tell the runtime exactly which DOM nodes a signal drives.

## Bindings, not diffs

A signal that feeds a text node becomes a direct write. A signal that feeds an
attribute becomes an attribute effect. There is no tree to reconcile, so
hydration is predictable and cheap.

## Delegated events

Most events are handled through a single delegated listener; events that do not
bubble (like `focus` and `change`) get direct listeners. Form inputs bind
through a dedicated property path.
