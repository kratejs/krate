---
title: Hello, Collections
description: The first typed content collection entry.
date: 2026-09-12
author: Krate
tags: [content, collections, typescript]
draft: false
order: 1
---

# Hello, Collections

This file lives in `src/content/blog` and is validated against the `blog`
collection schema declared in `krate.config.ts`. Because `order` is required, a
build fails if this entry omits it — and `getCollection('blog')` reads it with
full types during the build.
