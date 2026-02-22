#!/usr/bin/env node

import { spawn } from "node:child_process";
import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";

import { Desktop, start } from "../index";
import type { DesktopState } from "../session";

const BOOLEAN_FLAGS = new Set(["json", "no-openbox", "foreground", "allow-non-zero"]);

const defaultStateFile =
  process.env.PORTABLEDESKTOP_STATE_FILE || path.join(os.homedir(), ".cache", "portabledesktop", "session.json");

interface ParsedArgs {
  positional: string[];
  flags: Map<string, string | boolean>;
}

interface RecordingState {
  pid: number;
  file: string;
  startedAt: string;
}

interface StoredDesktopState extends DesktopState {
  stateFile: string;
  startedAt: string;
  recording: RecordingState | null;
}

interface OpenCommandResult {
  pid: number;
  command: string;
  args: string[];
}

interface CommandResult {
  code: number;
  signal: NodeJS.Signals | null;
  stdout: string;
  stderr: string;
}

function parseOptionValue(args: readonly string[], index: number, optionName: string): string {
  if (index + 1 >= args.length || args[index + 1].startsWith("--")) {
    throw new Error(`${optionName} requires a value`);
  }
  return args[index + 1];
}

function parseKeyValueArgs(args: readonly string[]): ParsedArgs {
  const positional: string[] = [];
  const flags = new Map<string, string | boolean>();

  for (let i = 0; i < args.length; i += 1) {
    const token = args[i];
    if (!token.startsWith("--")) {
      positional.push(token);
      continue;
    }

    const key = token.slice(2);
    if (BOOLEAN_FLAGS.has(key)) {
      flags.set(key, true);
      continue;
    }

    const value = parseOptionValue(args, i, token);
    flags.set(key, value);
    i += 1;
  }

  return { positional, flags };
}

function hasFlag(args: ParsedArgs, name: string): boolean {
  return args.flags.get(name) === true;
}

function getOption(args: ParsedArgs, name: string): string | undefined {
  const value = args.flags.get(name);
  return typeof value === "string" ? value : undefined;
}

function parseInteger(value: string, label: string): number {
  const parsed = Number.parseInt(value, 10);
  if (!Number.isFinite(parsed)) {
    throw new Error(`invalid integer for ${label}: ${value}`);
  }
  return parsed;
}

function parseIntegerOption(args: ParsedArgs, name: string): number | undefined {
  const value = getOption(args, name);
  return value === undefined ? undefined : parseInteger(value, `--${name}`);
}

function stateFileFrom(args: ParsedArgs): string {
  return path.resolve(getOption(args, "state-file") || defaultStateFile);
}

function nowIso(): string {
  return new Date().toISOString();
}

function desktopFromState(state: DesktopState): Desktop {
  return new Desktop({
    runtimeDir: state.runtimeDir,
    display: state.display,
    port: state.port,
    geometry: state.geometry,
    depth: state.depth,
    sessionDir: state.sessionDir,
    cleanupSessionDirOnStop: state.cleanupSessionDirOnStop,
    xvncPid: state.xvncPid,
    openboxPid: state.openboxPid,
    detached: state.detached
  });
}

async function terminatePid(pid: number, timeoutMs = 5000): Promise<void> {
  try {
    process.kill(pid, "SIGTERM");
  } catch (error) {
    const err = error as NodeJS.ErrnoException;
    if (err.code === "ESRCH") {
      return;
    }
    throw err;
  }

  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      process.kill(pid, 0);
      // eslint-disable-next-line no-await-in-loop
      await new Promise((resolve) => setTimeout(resolve, 100));
    } catch (error) {
      const err = error as NodeJS.ErrnoException;
      if (err.code === "ESRCH") {
        return;
      }
      throw err;
    }
  }

  try {
    process.kill(pid, "SIGKILL");
  } catch (error) {
    const err = error as NodeJS.ErrnoException;
    if (err.code !== "ESRCH") {
      throw err;
    }
  }
}

async function openCommand(
  desktop: Desktop,
  command: string,
  args: string[],
  options: { cwd?: string } = {}
): Promise<OpenCommandResult> {
  const child = spawn(command, args, {
    cwd: options.cwd || process.cwd(),
    env: desktop.env,
    detached: true,
    stdio: "ignore"
  });

  const spawnError = await Promise.race<Error | null>([
    new Promise((resolve) => {
      child.once("error", (error) => resolve(error as Error));
    }),
    new Promise<null>((resolve) => setTimeout(() => resolve(null), 30))
  ]);

  if (spawnError) {
    throw spawnError;
  }

  if (!child.pid) {
    throw new Error(`failed to launch command: ${command}`);
  }

  child.unref();
  return { pid: child.pid, command, args };
}

async function runCommand(
  desktop: Desktop,
  command: string,
  args: string[],
  options: { cwd?: string; timeoutMs?: number } = {}
): Promise<CommandResult> {
  const child = spawn(command, args, {
    cwd: options.cwd || process.cwd(),
    env: desktop.env,
    stdio: ["ignore", "pipe", "pipe"]
  });

  let stdout = "";
  let stderr = "";
  if (child.stdout) {
    child.stdout.on("data", (chunk: Buffer) => {
      stdout += chunk.toString();
    });
  }
  if (child.stderr) {
    child.stderr.on("data", (chunk: Buffer) => {
      stderr += chunk.toString();
    });
  }

  let timedOut = false;
  let timer: NodeJS.Timeout | null = null;
  if (options.timeoutMs && options.timeoutMs > 0) {
    timer = setTimeout(() => {
      timedOut = true;
      try {
        child.kill("SIGKILL");
      } catch {
        // ignore
      }
    }, options.timeoutMs);
  }

  const exit = await new Promise<{ code: number | null; signal: NodeJS.Signals | null }>((resolve, reject) => {
    child.once("error", reject);
    child.once("exit", (code, signal) => resolve({ code, signal }));
  });

  if (timer) {
    clearTimeout(timer);
  }
  if (timedOut) {
    throw new Error(`${command} timed out after ${options.timeoutMs}ms`);
  }

  return {
    code: typeof exit.code === "number" ? exit.code : -1,
    signal: exit.signal,
    stdout,
    stderr
  };
}

function isChromeLike(command: string): boolean {
  const base = path.basename(command).toLowerCase();
  return base.includes("chrome") || base.includes("chromium");
}

function assertObject(value: unknown, label: string): Record<string, unknown> {
  if (value === null || typeof value !== "object" || Array.isArray(value)) {
    throw new Error(`${label} must be an object`);
  }
  return value as Record<string, unknown>;
}

function readRequiredString(obj: Record<string, unknown>, field: string): string {
  const value = obj[field];
  if (typeof value !== "string" || value.length === 0) {
    throw new Error(`state field ${field} must be a non-empty string`);
  }
  return value;
}

function readOptionalString(obj: Record<string, unknown>, field: string): string | undefined {
  const value = obj[field];
  if (value === undefined || value === null) {
    return undefined;
  }
  if (typeof value !== "string" || value.length === 0) {
    throw new Error(`state field ${field} must be a non-empty string`);
  }
  return value;
}

function readRequiredNumber(obj: Record<string, unknown>, field: string): number {
  const value = obj[field];
  if (typeof value !== "number" || !Number.isFinite(value)) {
    throw new Error(`state field ${field} must be a finite number`);
  }
  return value;
}

function readOptionalNumber(obj: Record<string, unknown>, field: string): number | null {
  const value = obj[field];
  if (value === undefined || value === null) {
    return null;
  }
  if (typeof value !== "number" || !Number.isFinite(value)) {
    throw new Error(`state field ${field} must be a finite number or null`);
  }
  return value;
}

function readRequiredBoolean(obj: Record<string, unknown>, field: string): boolean {
  const value = obj[field];
  if (typeof value !== "boolean") {
    throw new Error(`state field ${field} must be a boolean`);
  }
  return value;
}

function readOptionalBoolean(obj: Record<string, unknown>, field: string): boolean | undefined {
  const value = obj[field];
  if (value === undefined || value === null) {
    return undefined;
  }
  if (typeof value !== "boolean") {
    throw new Error(`state field ${field} must be a boolean`);
  }
  return value;
}

function parseRecordingState(value: unknown): RecordingState | null {
  if (value === undefined || value === null) {
    return null;
  }

  const recording = assertObject(value, "recording");
  return {
    pid: readRequiredNumber(recording, "pid"),
    file: readRequiredString(recording, "file"),
    startedAt: readRequiredString(recording, "startedAt")
  };
}

function parseStoredState(value: unknown, stateFilePath: string): StoredDesktopState {
  const root = assertObject(value, "state");

  return {
    runtimeDir: readRequiredString(root, "runtimeDir"),
    display: readRequiredNumber(root, "display"),
    port: readRequiredNumber(root, "port"),
    geometry: readRequiredString(root, "geometry"),
    depth: readRequiredNumber(root, "depth"),
    sessionDir: readRequiredString(root, "sessionDir"),
    cleanupSessionDirOnStop: readOptionalBoolean(root, "cleanupSessionDirOnStop") ?? false,
    xvncPid: readOptionalNumber(root, "xvncPid"),
    openboxPid: readOptionalNumber(root, "openboxPid"),
    detached: readRequiredBoolean(root, "detached"),
    stateFile: readOptionalString(root, "stateFile") || stateFilePath,
    startedAt: readOptionalString(root, "startedAt") || nowIso(),
    recording: parseRecordingState(root.recording)
  };
}

async function loadState(stateFilePath: string): Promise<StoredDesktopState> {
  const raw = await fs.readFile(stateFilePath, "utf8");
  const parsed = JSON.parse(raw) as unknown;
  return parseStoredState(parsed, stateFilePath);
}

async function saveState(stateFilePath: string, state: StoredDesktopState): Promise<void> {
  await fs.mkdir(path.dirname(stateFilePath), { recursive: true });
  await fs.writeFile(stateFilePath, `${JSON.stringify(state, null, 2)}\n`);
}

async function cmdUp(rawArgs: readonly string[]): Promise<void> {
  const args = parseKeyValueArgs(rawArgs);
  const stateFilePath = stateFileFrom(args);
  const backgroundColor = getOption(args, "background");

  const desktop = await start({
    detached: !hasFlag(args, "foreground"),
    runtimeDir: getOption(args, "runtime-dir"),
    sessionDir: getOption(args, "session-dir"),
    display: getOption(args, "display"),
    port: getOption(args, "port"),
    geometry: getOption(args, "geometry"),
    depth: getOption(args, "depth"),
    openbox: !hasFlag(args, "no-openbox"),
    background: backgroundColor ? { color: backgroundColor } : undefined
  });

  const state: StoredDesktopState = {
    runtimeDir: desktop.runtimeDir,
    display: desktop.display,
    port: desktop.port,
    geometry: desktop.geometry,
    depth: desktop.depth,
    sessionDir: desktop.sessionDir,
    cleanupSessionDirOnStop: desktop.cleanupSessionDirOnStop,
    xvncPid: desktop.xvncPid,
    openboxPid: desktop.openboxPid,
    detached: desktop.detached,
    stateFile: stateFilePath,
    startedAt: nowIso(),
    recording: null
  };

  await saveState(stateFilePath, state);

  if (hasFlag(args, "json")) {
    process.stdout.write(`${JSON.stringify(state)}\n`);
  } else {
    process.stdout.write(`state: ${stateFilePath}\n`);
    process.stdout.write(`display: :${state.display}\n`);
    process.stdout.write(`vnc: 127.0.0.1:${state.port}\n`);
    process.stdout.write(`runtime: ${state.runtimeDir}\n`);
    process.stdout.write(`session: ${state.sessionDir}\n`);
  }

  if (!hasFlag(args, "foreground")) {
    return;
  }

  let stopping = false;
  const stopAndExit = (): void => {
    if (stopping) {
      return;
    }
    stopping = true;
    void (async () => {
      try {
        await desktop.kill();
      } finally {
        await fs.rm(stateFilePath, { force: true });
        process.exit(0);
      }
    })();
  };

  process.on("SIGINT", stopAndExit);
  process.on("SIGTERM", stopAndExit);

  await new Promise<void>(() => {
    // Keep the foreground process alive until it receives a signal.
  });
}

async function cmdDown(rawArgs: readonly string[]): Promise<void> {
  const args = parseKeyValueArgs(rawArgs);
  const stateFilePath = stateFileFrom(args);
  const state = await loadState(stateFilePath);

  if (state.recording?.pid) {
    await terminatePid(state.recording.pid).catch(() => {});
  }

  const desktop = desktopFromState(state);
  await desktop.kill();

  await fs.rm(stateFilePath, { force: true });
  process.stdout.write("stopped\n");
}

async function cmdInfo(rawArgs: readonly string[]): Promise<void> {
  const args = parseKeyValueArgs(rawArgs);
  const state = await loadState(stateFileFrom(args));

  if (hasFlag(args, "json")) {
    process.stdout.write(`${JSON.stringify(state)}\n`);
    return;
  }

  process.stdout.write(`state: ${state.stateFile}\n`);
  process.stdout.write(`display: :${state.display}\n`);
  process.stdout.write(`vnc: 127.0.0.1:${state.port}\n`);
  process.stdout.write(`runtime: ${state.runtimeDir}\n`);
  process.stdout.write(`session: ${state.sessionDir}\n`);
  process.stdout.write(`started: ${state.startedAt}\n`);
  process.stdout.write(`recording: ${state.recording ? state.recording.file : "none"}\n`);
}

async function cmdOpen(rawArgs: readonly string[]): Promise<void> {
  const separatorIndex = rawArgs.indexOf("--");
  const before = separatorIndex === -1 ? rawArgs : rawArgs.slice(0, separatorIndex);
  const after = separatorIndex === -1 ? [] : rawArgs.slice(separatorIndex + 1);

  const args = parseKeyValueArgs(before);
  const state = await loadState(stateFileFrom(args));
  const desktop = desktopFromState(state);

  const commandParts = after.length > 0 ? after : args.positional;
  if (commandParts.length === 0) {
    throw new Error("open requires a command. Example: portabledesktop open -- google-chrome-stable https://example.com");
  }

  const [command, ...commandArgs] = commandParts;
  if (isChromeLike(command) && !commandArgs.some((arg) => arg.startsWith("--user-data-dir"))) {
    const profileDir = path.join(state.sessionDir, "profiles", `chrome-${Date.now()}`);
    await fs.mkdir(profileDir, { recursive: true });
    commandArgs.push(`--user-data-dir=${profileDir}`);
  }

  const launched = await openCommand(desktop, command, commandArgs, {
    cwd: getOption(args, "cwd")
  });

  process.stdout.write(`${JSON.stringify(launched)}\n`);
}

async function cmdRun(rawArgs: readonly string[]): Promise<void> {
  const separatorIndex = rawArgs.indexOf("--");
  const before = separatorIndex === -1 ? rawArgs : rawArgs.slice(0, separatorIndex);
  const after = separatorIndex === -1 ? [] : rawArgs.slice(separatorIndex + 1);

  const args = parseKeyValueArgs(before);
  const state = await loadState(stateFileFrom(args));
  const desktop = desktopFromState(state);

  const commandParts = after.length > 0 ? after : args.positional;
  if (commandParts.length === 0) {
    throw new Error("run requires a command. Example: portabledesktop run -- xdotool getmouselocation");
  }

  const [command, ...commandArgs] = commandParts;
  const result = await runCommand(desktop, command, commandArgs, {
    cwd: getOption(args, "cwd"),
    timeoutMs: parseIntegerOption(args, "timeout-ms")
  });

  if (result.code !== 0 && !hasFlag(args, "allow-non-zero")) {
    throw new Error(`${command} exited with code ${result.code}: ${result.stderr || result.stdout}`.trim());
  }

  if (hasFlag(args, "json")) {
    process.stdout.write(`${JSON.stringify(result)}\n`);
  } else {
    if (result.stdout) {
      process.stdout.write(result.stdout);
    }
    if (result.stderr) {
      process.stderr.write(result.stderr);
    }
    if (result.code !== 0) {
      process.stderr.write(`command exited with code ${result.code}\n`);
    }
  }

  if (result.code !== 0) {
    process.exitCode = result.code;
  }
}

async function cmdBackground(rawArgs: readonly string[]): Promise<void> {
  const args = parseKeyValueArgs(rawArgs);
  const color = args.positional[0] || getOption(args, "color");
  if (!color) {
    throw new Error("background requires a color. Example: portabledesktop background '#202428'");
  }

  const state = await loadState(stateFileFrom(args));
  const desktop = desktopFromState(state);
  await desktop.setBackground(color);
  process.stdout.write("background updated\n");
}

async function cmdMouse(rawArgs: readonly string[]): Promise<void> {
  const args = parseKeyValueArgs(rawArgs);
  const [subcommand, ...rest] = args.positional;
  if (!subcommand) {
    throw new Error("mouse command required: move|click|down|up|scroll");
  }

  const state = await loadState(stateFileFrom(args));
  const desktop = desktopFromState(state);

  switch (subcommand) {
    case "move":
      await desktop.moveMouse(parseInteger(rest[0] ?? "", "move x"), parseInteger(rest[1] ?? "", "move y"));
      break;
    case "click":
      await desktop.click(rest[0] || "left");
      break;
    case "down":
      await desktop.mouseDown(rest[0] || "left");
      break;
    case "up":
      await desktop.mouseUp(rest[0] || "left");
      break;
    case "scroll":
      await desktop.scroll(
        rest[0] === undefined ? 0 : parseInteger(rest[0], "scroll dx"),
        rest[1] === undefined ? 0 : parseInteger(rest[1], "scroll dy")
      );
      break;
    default:
      throw new Error(`unknown mouse subcommand: ${subcommand}`);
  }

  process.stdout.write("ok\n");
}

async function cmdKeyboard(rawArgs: readonly string[]): Promise<void> {
  const args = parseKeyValueArgs(rawArgs);
  const [subcommand, ...rest] = args.positional;
  if (!subcommand) {
    throw new Error("keyboard command required: type|key");
  }

  const state = await loadState(stateFileFrom(args));
  const desktop = desktopFromState(state);

  switch (subcommand) {
    case "type":
      await desktop.type(rest.join(" "));
      break;
    case "key":
      await desktop.key(rest.join(" "));
      break;
    default:
      throw new Error(`unknown keyboard subcommand: ${subcommand}`);
  }

  process.stdout.write("ok\n");
}

async function cmdRecord(rawArgs: readonly string[]): Promise<void> {
  const args = parseKeyValueArgs(rawArgs);
  const [subcommand, ...rest] = args.positional;
  if (!subcommand) {
    throw new Error("record command required: start|stop");
  }

  const stateFilePath = stateFileFrom(args);
  const state = await loadState(stateFilePath);
  const desktop = desktopFromState(state);

  if (subcommand === "start") {
    if (state.recording?.pid) {
      throw new Error("recording already in progress");
    }

    const outFile = rest[0] || path.join(state.sessionDir, `recording-${Date.now()}.mp4`);
    const recording = await desktop.record({
      file: outFile,
      fps: parseIntegerOption(args, "fps") ?? 30,
      detached: true
    });

    state.recording = {
      pid: recording.pid,
      file: recording.file,
      startedAt: nowIso()
    };
    await saveState(stateFilePath, state);
    process.stdout.write(`${JSON.stringify(state.recording)}\n`);
    return;
  }

  if (subcommand === "stop") {
    if (!state.recording?.pid) {
      process.stdout.write("no recording active\n");
      return;
    }

    await terminatePid(state.recording.pid).catch(() => {});
    state.recording = null;
    await saveState(stateFilePath, state);
    process.stdout.write("recording stopped\n");
    return;
  }

  throw new Error(`unknown record subcommand: ${subcommand}`);
}

function printHelp(): void {
  process.stdout.write("portabledesktop\n\n");
  process.stdout.write("Usage:\n");
  process.stdout.write("  portabledesktop up [--port 5901] [--geometry 1280x800] [--background '#202428'] [--json]\n");
  process.stdout.write("  portabledesktop down\n");
  process.stdout.write("  portabledesktop info [--json]\n");
  process.stdout.write("  portabledesktop open -- <command> [args...]\n");
  process.stdout.write("  portabledesktop run [--json] [--allow-non-zero] -- <command> [args...]\n");
  process.stdout.write("  portabledesktop background <color>\n");
  process.stdout.write("  portabledesktop mouse move <x> <y>\n");
  process.stdout.write("  portabledesktop mouse click [left|middle|right]\n");
  process.stdout.write("  portabledesktop keyboard type <text>\n");
  process.stdout.write("  portabledesktop keyboard key <combo>\n");
  process.stdout.write("  portabledesktop record start [file.mp4]\n");
  process.stdout.write("  portabledesktop record stop\n");
  process.stdout.write("\nGlobal state file override:\n");
  process.stdout.write("  --state-file <path>\n");
}

async function main(): Promise<void> {
  const [, , command, ...args] = process.argv;

  if (!command || command === "-h" || command === "--help" || command === "help") {
    printHelp();
    return;
  }

  switch (command) {
    case "up":
      await cmdUp(args);
      return;
    case "down":
      await cmdDown(args);
      return;
    case "info":
      await cmdInfo(args);
      return;
    case "open":
      await cmdOpen(args);
      return;
    case "run":
      await cmdRun(args);
      return;
    case "background":
      await cmdBackground(args);
      return;
    case "mouse":
      await cmdMouse(args);
      return;
    case "keyboard":
      await cmdKeyboard(args);
      return;
    case "record":
      await cmdRecord(args);
      return;
    default:
      throw new Error(`unknown command: ${command}`);
  }
}

main().catch((error: unknown) => {
  const message = error instanceof Error ? error.message : String(error);
  process.stderr.write(`error: ${message}\n`);
  process.exit(1);
});
