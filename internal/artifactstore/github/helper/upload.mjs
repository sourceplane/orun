import { createRequire } from 'module';
const require = createRequire(import.meta.url);
const { UploadArtifactClient } = require('@actions/artifact');

async function main() {
  const [shardDir, artifactName, retentionDays] = process.argv.slice(2);

  if (!shardDir || !artifactName) {
    console.error('Usage: node upload.mjs <shardDir> <artifactName> [retentionDays]');
    process.exit(1);
  }

  const client = new UploadArtifactClient();
  const options = {};
  if (retentionDays) {
    options.retentionDays = parseInt(retentionDays, 10);
  }

  const result = await client.uploadArtifact(artifactName, shardDir, options);

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