import crypto from "node:crypto";
import fs from "node:fs";
import fsp from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import * as tar from "tar";

export type RuntimeSource = "explicit" | "bundled-cache" | "dev-result";

export interface RuntimeInfo {
  runtimeDir: string;
  manifestPath: string | null;
  source: RuntimeSource;
}

export interface EnsureRuntimeOptions {
  runtimeDir?: string;
}

interface BundledManifest {
  archive_sha256?: string;
}

interface ExtractBundledRuntimeParams {
  runtimeCacheDir: string;
  runtimeHash: string;
}

const moduleDir = path.dirname(fileURLToPath(import.meta.url));
const packageRoot = path.resolve(moduleDir, "..");

const bundledTarPath = path.join(packageRoot, "assets", "output.tar");
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

async function sha256File(filePath: string): Promise<string> {
  const hash = crypto.createHash("sha256");
  await new Promise<void>((resolve, reject) => {
    const stream = fs.createReadStream(filePath);
    stream.on("data", (chunk: string | Buffer) => {
      hash.update(chunk);
    });
    stream.on("error", reject);
    stream.on("end", () => resolve());
  });
  return hash.digest("hex");
}

async function readManifest(filePath: string): Promise<BundledManifest> {
  const raw = await fsp.readFile(filePath, "utf8");
  return JSON.parse(raw) as BundledManifest;
}

async function ensureDir(filePath: string): Promise<void> {
  await fsp.mkdir(filePath, { recursive: true });
}

function validateRuntimeDir(runtimeDir: string): void {
  const xvncPath = path.join(runtimeDir, "bin", "Xvnc");
  if (!existsPath(xvncPath)) {
    throw new Error(`runtime directory is missing bin/Xvnc: ${runtimeDir}`);
  }
}

async function extractBundledRuntime(params: ExtractBundledRuntimeParams): Promise<RuntimeInfo> {
  const { runtimeCacheDir, runtimeHash } = params;
  const runtimeRoot = path.join(runtimeCacheDir, runtimeHash);
  const runtimeDir = path.join(runtimeRoot, "output");
  const markerPath = path.join(runtimeRoot, ".ready");
  const manifestPath = path.join(runtimeRoot, "manifest.json");

  if (existsPath(markerPath) && existsPath(path.join(runtimeDir, "bin", "Xvnc"))) {
    return {
      runtimeDir,
      manifestPath,
      source: "bundled-cache"
    };
  }

  await ensureDir(runtimeCacheDir);
  const tempRoot = path.join(runtimeCacheDir, `.tmp-${process.pid}-${Date.now()}`);
  const tempRuntimeDir = path.join(tempRoot, "output");

  await ensureDir(tempRuntimeDir);
  await tar.x({
    file: bundledTarPath,
    cwd: tempRuntimeDir
  });

  const bundledManifestRaw = await fsp.readFile(bundledManifestPath, "utf8");
  await fsp.writeFile(path.join(tempRoot, "manifest.json"), bundledManifestRaw);
  await fsp.writeFile(path.join(tempRoot, ".ready"), "ready\n");

  try {
    await fsp.rename(tempRoot, runtimeRoot);
  } catch (error) {
    const err = error as NodeJS.ErrnoException;
    if (err.code === "EEXIST") {
      await fsp.rm(tempRoot, { recursive: true, force: true });
    } else {
      throw err;
    }
  }

  validateRuntimeDir(runtimeDir);
  return {
    runtimeDir,
    manifestPath,
    source: "bundled-cache"
  };
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

  if (existsPath(bundledTarPath) && existsPath(bundledManifestPath)) {
    const bundledManifest = await readManifest(bundledManifestPath);
    const runtimeHash = bundledManifest.archive_sha256 || (await sha256File(bundledTarPath));
    const cacheDir = path.resolve(
      expandHome(process.env.PORTABLEDESKTOP_CACHE_DIR || path.join(os.homedir(), ".cache", "portabledesktop"))
    );

    return extractBundledRuntime({
      runtimeCacheDir: cacheDir,
      runtimeHash
    });
  }

  if (existsPath(devResultOutputPath)) {
    validateRuntimeDir(devResultOutputPath);
    return {
      runtimeDir: path.resolve(devResultOutputPath),
      manifestPath: existsPath(devResultManifestPath) ? path.resolve(devResultManifestPath) : null,
      source: "dev-result"
    };
  }

  throw new Error(
    "runtime assets not found. Expected assets/output.tar + assets/manifest.json or result/output from a local nix build"
  );
}

export function resolveRuntimeBinary(runtimeDir: string, binaryName: string): string {
  const candidate = path.join(runtimeDir, "bin", binaryName);
  if (existsPath(candidate)) {
    return candidate;
  }
  return binaryName;
}
