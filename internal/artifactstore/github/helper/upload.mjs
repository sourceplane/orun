import { createRequire } from 'module';
const require = createRequire(import.meta.url);
const { DefaultArtifactClient } = require('@actions/artifact');
import { readdirSync } from 'fs';
import { join } from 'path';

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

  // v2 @actions/artifact API: uploadArtifact(name, files[], rootDirectory, options)
  const files = readdirSync(shardDir).map(f => join(shardDir, f));
  const result = await client.uploadArtifact(artifactName, files, shardDir, options);

  console.log(JSON.stringify({
    id: result.id,
    name: artifactName,
    size: result.size,
  }));
}

main().catch(e => {
  console.error(e.message);
  process.exit(1);
});