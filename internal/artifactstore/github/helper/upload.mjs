import { createRequire } from 'module';
const require = createRequire(import.meta.url);
const { DefaultArtifactClient } = require('@actions/artifact');
import { readdirSync, statSync } from 'fs';
import { join } from 'path';

function getAllFiles(dir) {
  const results = [];
  for (const entry of readdirSync(dir)) {
    const full = join(dir, entry);
    if (statSync(full).isDirectory()) {
      results.push(...getAllFiles(full));
    } else {
      results.push(full);
    }
  }
  return results;
}

async function main() {
  const [shardDir, artifactName, retentionDays] = process.argv.slice(2);

  if (!shardDir || !artifactName) {
    console.error('Usage: node upload.mjs <shardDir> <artifactName> [retentionDays]');
    process.exit(1);
  }

  const client = new DefaultArtifactClient();
  const options = {};
  if (retentionDays) {
    options.retentionDays = parseInt(retentionDays, 10);
  }

  const files = getAllFiles(shardDir);
  const result = await client.uploadArtifact(artifactName, files, shardDir, options);

  console.log(JSON.stringify({
    id: String(result.id),
    name: artifactName,
    size: result.size,
  }));
}

main().catch(e => {
  console.error(e.message);
  process.exit(1);
});