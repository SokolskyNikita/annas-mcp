#!/usr/bin/env node

import { isDirectRun, main } from "../lib/launcher.js";

if (isDirectRun(import.meta.url)) {
  main({ defaultArgs: [], argv0: "annas-cli" }).catch((error) => {
    console.error(error.message);
    process.exit(1);
  });
}
