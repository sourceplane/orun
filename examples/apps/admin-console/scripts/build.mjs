import { cp, mkdir, rm } from "node:fs/promises";

const sourceDir = new URL("../src/", import.meta.url);
const outputDir = new URL("../dist/", import.meta.url);

await rm(outputDir, { force: true, recursive: true });
await mkdir(outputDir, { recursive: true });
await cp(sourceDir, outputDir, { recursive: true });