#!/usr/bin/env node

import { isDirectRun, main } from "../lib/launcher.js";

if (isDirectRun(import.meta.url)) {
  main().catch((error) => {
    console.error(error.message);
    process.exit(1);
  });
}
