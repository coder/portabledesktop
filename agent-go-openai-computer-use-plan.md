# Plan: Add OpenAI Computer Use to `examples/agent-go`

## Context

`examples/agent-go/main.go` currently runs a manual fantasy agent loop hard-coded to Anthropic's computer use tool. Fantasy commit
[`56a1356`](https://github.com/hugodutka/fantasy/commit/56a1356eec1812d7dfd27c567d2b9e3388070306)
adds a parallel OpenAI computer use surface in `providers/openai/computer_use.go`:

- `openai.NewComputerUseTool(run)` returns a `fantasy.ExecutableProviderTool`
  whose `Run` is invoked through the standard tool-call flow.
- Tool input is a JSON envelope `{ "call_id": "...", "actions": [...] }`. Each
  action is a `responses.ComputerActionUnion` from `charm.land/openai-go/responses`
  with `AsClick`, `AsDoubleClick`, `AsDrag`, `AsKeypress`, `AsMove`, `AsScreenshot`,
  `AsScroll`, `AsType`, and `AsWait` accessors.
- Action coordinates are absolute pixels in the declared display size we hand to
  the model. There is no zoom action — coordinates always refer to the full
  declared display.
- `openai.ComputerUseMetadata` round-trips raw `computer_call` JSON. The provider
  needs it on every assistant `ToolCallPart` re-sent in follow-up requests, and
  exposes it on inbound `ToolCallContent.ProviderMetadata`. We must propagate it
  back as `ToolCallPart.ProviderOptions` so the response replays correctly.
- The OpenAI `computer_call_output` only accepts an image data URI. The provider
  uses `ToolResultOutputContentMedia{Data: <base64>, MediaType: "image/..."}` to
  build it.
- Tool registration also requires `openai.WithUseResponsesAPI()` and the
  `computer` tool name.

The same commit also bumps `NewComputerUseTool` for Anthropic to take a `run`
callback — the existing agent-go example calls the older two-arg form, so the
build is currently broken until we adapt.

The replace directive in `examples/agent-go/go.mod` is the only place pinning
fantasy. We pin it to the commit's pseudo-version
`v0.0.0-20260427112804-56a1356eec18`.

## Goals

1. Update fantasy to commit
   `56a1356eec1812d7dfd27c567d2b9e3388070306` (pseudo-version
   `v0.0.0-20260427112804-56a1356eec18`).
2. Keep the existing Anthropic computer-use flow working with the new API.
3. Add an OpenAI computer-use code path selectable via flags so a single binary
   can run either provider.
4. Keep coordinates in the existing declared/native scaling pipeline so
   click-probe and screenshot logic stay correct.

## Design

### Provider selection

Add `--provider` (`anthropic`|`openai`) and a `defaultOpenAIModel` constant
(start with `gpt-5.4`, matching fantasy's tests). Default provider stays
`anthropic` to preserve the current default behavior. When `--provider=openai`
is selected, the example uses `OPENAI_API_KEY`.

The `--model` default depends on the provider: keep `claude-opus-4-6` when the
provider is `anthropic`, switch to `gpt-5.4` for `openai`. This is implemented
by leaving the flag default empty and resolving the effective model after flag
parse.

### Action execution

Refactor the existing Anthropic-only action handling into per-provider files.
Both providers map onto the same `desktop.ExecVoid("mouse" | "keyboard", ...)`
and `desktop.Exec("screenshot", ...)` primitives.

Anthropic actions stay in `actions_anthropic.go` (renamed from inline
`executeComputerAction`). The screenshot helper that handles cropping
remains shared.

OpenAI actions live in a new `actions_openai.go`:

| OpenAI action      | Implementation                                                     |
|--------------------|--------------------------------------------------------------------|
| `screenshot`       | capture and return base64 PNG                                      |
| `click`            | move to (x,y); `mouse click <button>`                              |
| `double_click`     | move to (x,y); `mouse click left` twice                            |
| `move`             | `mouse move x y`                                                   |
| `scroll`           | move to (x,y); `mouse scroll <scrollX> <scrollY>` (sign preserved) |
| `type`             | `keyboard type <text>`                                             |
| `keypress`         | combine keys with `+` and call `keyboard key <combo>`              |
| `drag`             | move to first path point; `mouse down left`; iterate path moving along; `mouse up left` |
| `wait`             | sleep for ~1s (fantasy/openai default — no duration field)         |

Each action returns a screenshot to feed back into the model, except the
ones that the provider treats as needing no output. We always return a
screenshot to keep parity with the Anthropic flow, since the OpenAI provider
serializes `computer_call_output` as an image-only payload anyway.

`mouse click` only knows `left | middle | right` today. OpenAI's `click`
button can also be `wheel`, `back`, `forward`. Map `wheel` to `middle`.
For `back` and `forward` return an error from the dispatcher and let the
model see it as the tool result; do not silently fall through to a left
click.

### Message loop

Keep the manual `runAgentLoop` design to preserve existing tracing/metrics
and the messages JSON output. Three changes:

1. Tools become `[]fantasy.Tool{tool}` where `tool` is now generated from a
   provider-specific helper that returns
   `(fantasy.Tool, ToolDispatcher)`. `ToolDispatcher` is a small interface:

   ```go
   type computerToolDispatcher interface {
       Tool() fantasy.Tool
       Execute(ctx context.Context, call fantasy.ToolCallContent, metrics *reporting.AgentMetrics) (fantasy.ToolResultOutputContent, fantasy.ProviderOptions, string, error)
   }
   ```

   The third return is a human-readable label printed for logs (mirrors the
   current `reporting.FormatAction` output). The dispatcher returns
   `ProviderOptions` only when the provider requires a follow-up echo on the
   `ToolResultPart` (currently neither does).

2. When echoing assistant tool calls back into the prompt, copy
   `ProviderMetadata` into `ToolCallPart.ProviderOptions`. This is the OpenAI
   round-trip requirement; for Anthropic it is a harmless copy (the map is
   either empty or holds anthropic metadata that's also expected back).

3. For OpenAI we additionally need to keep reasoning content in the assistant
   message. Iterate `resp.Content` and copy `fantasy.ReasoningContent` parts
   into `fantasy.ReasoningPart` with their `ProviderMetadata` →
   `ProviderOptions` (encrypted reasoning tokens are supplied by the OpenAI
   provider via metadata). Anthropic doesn't emit reasoning under this flow,
   so the same code path is a no-op there.

4. Pass `ProviderOptions` to the call when running OpenAI so we can request
   `medium` reasoning effort via `openai.ResponsesProviderOptions{
   ReasoningEffort: openai.ReasoningEffortOption(openai.ReasoningEffortMedium)
   }`. Anthropic gets `nil` provider options.

### Anthropic adapter shape

`anthropic.NewComputerUseTool` now takes a `run` callback. We keep
`executeComputerAction` and wrap it so the new callback is purely a
shim that:

1. parses input via `anthropic.ParseComputerUseInput`,
2. invokes the existing executor,
3. converts the first `ToolResultOutputContent` into the `ToolResponse` shape
   (image/png screenshots → `fantasy.NewImageResponse`, text errors →
   `fantasy.ToolResponse{Type: "text", Content: text, IsError: true}`).

Even though the executable callback now exists, we still drive the tool call
loop manually (we want metrics, message dumps, and screenshots saved on disk).
The callback path is unused at runtime for the manual loop — the dispatcher
sees the raw input string in `ToolCallContent.Input`. We still register the
callback so the tool stays valid when fantasy validates the registration.

For Anthropic the provider already handles raw input fine; we keep
`executeComputerAction` wired through the dispatcher.

### Coordinate scaling for OpenAI

Anthropic exposes a separate "declared" size via `DisplayWidthPx/Px`. The
OpenAI computer tool has no such knob — coordinates come back at whatever
resolution the model chose. The fantasy provider/SDK doesn't currently
publish display dimensions; the model just sees screenshots and emits
coordinates relative to those screenshots.

Since our screenshots are returned scaled to `activeGeometry.DeclaredWidth/
Height` (we drive `--target-width/--target-height` to those), the coordinates
we receive are in declared space. We pass them through
`activeGeometry.DeclaredPointToNative` exactly like the Anthropic flow, so the
existing scaling pipeline keeps working.

We do not pass a separate display size to the OpenAI provider; the model
infers it from the screenshot it receives.

### Fantasy version bump

Replace directive in `go.mod` updated to:

```
replace charm.land/fantasy => github.com/hugodutka/fantasy v0.0.0-20260427112804-56a1356eec18
```

`go mod tidy` is then required to refresh `go.sum`.

### README update

Add a small "OpenAI" section describing `--provider openai`,
`OPENAI_API_KEY`, and the default model.

## Files touched

- `examples/agent-go/go.mod` — bump fantasy via `replace` directive.
- `examples/agent-go/go.sum` — regenerated by `go mod tidy`.
- `examples/agent-go/main.go` — provider selection, manual loop changes,
  shared dispatcher.
- `examples/agent-go/actions_anthropic.go` (new) — extracted Anthropic
  executor + dispatcher.
- `examples/agent-go/actions_openai.go` (new) — OpenAI executor +
  dispatcher.
- `examples/agent-go/README.md` — OpenAI documentation.

`reporting`, `display`, `desktop`, and `clickprobe` packages stay
unchanged.

## Verification

1. `go build ./...` (already gated by the existing module setup).
2. `go vet ./...`.
3. Spot-test that the binary still defaults to Anthropic and accepts
   `--provider openai`. We can't run a full end-to-end agent without
   real API keys, but `--help` should display the new flag and the
   binary should fail with a clear message when neither key is set.

## Out of scope

- VCR/test cassette parity with the fantasy repo's tests.
- Streaming output for OpenAI (manual loop uses `Generate`).
- New display sizes or multi-display support.

## Todos

- [ ] Bump fantasy in `examples/agent-go/go.mod` to commit `56a1356`
      (pseudo-version `v0.0.0-20260427112804-56a1356eec18`) and refresh
      `go.sum` via `go mod tidy`.
- [ ] Add `--provider` flag and provider-aware default for `--model`
      in `examples/agent-go/main.go`.
- [ ] Extract Anthropic action handling into
      `examples/agent-go/actions_anthropic.go` and adapt to fantasy's
      new `NewComputerUseTool(opts, run)` signature.
- [ ] Add OpenAI action handling in
      `examples/agent-go/actions_openai.go`, including a dispatcher
      that parses `openai.ComputerUseInput` and executes each action
      via `desktop.ExecVoid`.
- [ ] Refactor `runAgentLoop` to use a `computerToolDispatcher`
      interface so the loop is provider-agnostic, including copying
      `ProviderMetadata` into `ToolCallPart.ProviderOptions` and
      preserving reasoning parts.
- [ ] Wire OpenAI provider options (`ReasoningEffort: medium`) into
      `fantasy.Call.ProviderOptions` when running OpenAI; pass `nil`
      for Anthropic.
- [ ] Update `examples/agent-go/README.md` with a short OpenAI
      section.
- [ ] Build and `go vet ./...` from `examples/agent-go`.
- [ ] Sanity-check `--help` output and missing-API-key error
      messaging.
