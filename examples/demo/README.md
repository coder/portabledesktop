# examples/demo

Containerized demo that runs an Anthropic computer-use loop on a live portable desktop.
The image launches Chromium inside the desktop session.

## Build locally

Run these commands from the repository root:

```bash
pack_file="$(npm pack --silent | tail -n 1)"
mv "${pack_file}" portabledesktop.tgz
docker build -f examples/demo/Dockerfile -t portabledesktop-demo .
```

## Run

```bash
export ANTHROPIC_API_KEY=<key>
```

```bash
docker run -it --rm \
  -e ANTHROPIC_API_KEY="$ANTHROPIC_API_KEY" \
  -p 5190:5190 \
  portabledesktop-demo \
  "play a game of chess"
```

Open the printed viewer URL in your browser (usually `http://localhost:5190`).

If you want to clean up the local tarball after testing:

```bash
rm -f portabledesktop.tgz
```
