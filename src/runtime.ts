import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

export type RuntimeSource = "explicit" | "bundled-package" | "dev-result";

export interface RuntimeInfo {
  runtimeDir: string;
  manifestPath: string | null;
  source: RuntimeSource;
}

export interface EnsureRuntimeOptions {
  runtimeDir?: string;
}

const moduleDir = path.dirname(fileURLToPath(import.meta.url));
const packageRoot = path.resolve(moduleDir, "..");

const bundledOutputPath = path.join(packageRoot, "assets", "output");
const bundledManifestPath = path.join(packageRoot, "assets", "manifest.json");
const devResultOutputPath = path.join(packageRoot, "result", "output");
const devResultManifestPath = path.join(packageRoot, "result", "manifest.json");

function existsPath(filePath: string): boolean {
  return fs.existsSync(filePath);
}

function expandHome(inputPath: string): string {
  if (inputPath === "~") {
    return os.homedir();
  }
  if (inputPath.startsWith("~/")) {
    return path.join(os.homedir(), inputPath.slice(2));
  }
  return inputPath;
}

function validateRuntimeDir(runtimeDir: string): void {
  const xvncPath = path.join(runtimeDir, "bin", "Xvnc");
  if (!existsPath(xvncPath)) {
    throw new Error(`runtime directory is missing bin/Xvnc: ${runtimeDir}`);
  }
}

export async function ensureRuntime(options: EnsureRuntimeOptions = {}): Promise<RuntimeInfo> {
  const explicitRuntimeDirRaw = options.runtimeDir || process.env.PORTABLEDESKTOP_RUNTIME_DIR || "";
  if (explicitRuntimeDirRaw) {
    const resolved = path.resolve(expandHome(explicitRuntimeDirRaw));
    validateRuntimeDir(resolved);
    return {
      runtimeDir: resolved,
      manifestPath: null,
      source: "explicit"
    };
  }

  if (existsPath(bundledOutputPath)) {
    validateRuntimeDir(bundledOutputPath);
    return {
      runtimeDir: path.resolve(bundledOutputPath),
      manifestPath: existsPath(bundledManifestPath) ? path.resolve(bundledManifestPath) : null,
      source: "bundled-package"
    };
  }

  if (existsPath(devResultOutputPath)) {
    validateRuntimeDir(devResultOutputPath);
    return {
      runtimeDir: path.resolve(devResultOutputPath),
      manifestPath: existsPath(devResultManifestPath) ? path.resolve(devResultManifestPath) : null,
      source: "dev-result"
    };
  }

  throw new Error("runtime assets not found. Expected assets/output or result/output from a local nix build");
}

export function resolveRuntimeBinary(runtimeDir: string, binaryName: string): string {
  const candidate = path.join(runtimeDir, "bin", binaryName);
  if (existsPath(candidate)) {
    return candidate;
  }
  return binaryName;
}
