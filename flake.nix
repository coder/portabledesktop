{
  description = "PortableDesktop runtime packaging and asset pipeline";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachSystem [ "x86_64-linux" "aarch64-linux" ] (system:
      let
        pkgs = import nixpkgs { inherit system; };
        # Static resolution needs to evaluate unsupported pkgsStatic entries so
        # we can attempt real builds in the strict-static profile.
        pkgsForStaticResolution = import nixpkgs {
          inherit system;
          config.allowUnsupportedSystem = true;
        };
        lib = pkgs.lib;
        staticExcludedDepNames = [
          "dri-pkgconfig-stub"
          "fltk"
          "fltk-static"
          "glu"
          "libdrm"
          "libepoxy"
          "libglvnd"
          "libglvnd-static"
          "mesa-libgbm"
        ];
        # Tigervnc's embedded xserver configure disables DRI/GLX paths, so
        # static-only builds must avoid pulling shared-only GL/GBM closures.
        isAllowedStaticDep = pkg:
          !(lib.elem (lib.getName pkg) staticExcludedDepNames);
        staticLibxcvt = pkgsForStaticResolution.pkgsStatic.libxcvt.overrideAttrs (old: {
          # libxcvt hardcodes shared_library(), which fails under static stdenv.
          postPatch = (old.postPatch or "") + ''
            substituteInPlace lib/meson.build \
              --replace-fail "shared_library('xcvt'," "library('xcvt',"
          '';
        });
        staticXorgServer = (pkgsForStaticResolution.pkgsStatic.xorg-server.override {
          libxcvt = staticLibxcvt;
        }).overrideAttrs (old: {
          buildInputs = builtins.filter
            isAllowedStaticDep
            old.buildInputs;
          propagatedBuildInputs = builtins.filter
            isAllowedStaticDep
            (old.propagatedBuildInputs or [ ]);
        });
        staticTigerVNC = (pkgsForStaticResolution.pkgsStatic.tigervnc.override {
          waylandSupport = false;
          ffmpeg = pkgsForStaticResolution.ffmpeg-headless;
          "xorg-server" = pkgsForStaticResolution.xorg-server;
        }).overrideAttrs (old: {
          cmakeFlags = (old.cmakeFlags or [ ]) ++ [ "-DBUILD_VIEWER=OFF" ];
          configureFlags =
            let
              replacedFlags = builtins.map
                (flag: lib.replaceStrings [ "--enable-glx" ] [ "--disable-glx" ] flag)
                (old.configureFlags or [ ]);
            in
            if builtins.any (flag: flag == "--disable-glx") replacedFlags then
              replacedFlags
            else
              replacedFlags ++ [ "--disable-glx" ];
          buildInputs = builtins.filter
            (pkg:
              let
                name = lib.getName pkg;
              in
              isAllowedStaticDep pkg && name != "fltk" && name != "fltk-static")
            old.buildInputs;
          propagatedBuildInputs = builtins.filter
            isAllowedStaticDep
            (lib.flatten (old.propagatedBuildInputs or [ ]));
          postBuild = lib.replaceStrings
              [ "--disable-dri --disable-dri2 --disable-dri3 --enable-glx \\" ]
              [ "--disable-dri --disable-dri2 --disable-dri3 \\" ]
              (old.postBuild or "");
        });
        runtimeTigerVNC = (pkgs.tigervnc.override {
          ffmpeg = runtimeFfmpegRecorder;
        }).overrideAttrs (old: {
          # Build Xvnc with a PATH-resolved xkbcomp invocation instead of
          # embedding a /nix/store xkbcomp directory.
          cmakeFlags = (old.cmakeFlags or [ ]) ++ [
            "-DENABLE_GNUTLS=OFF"
          ];
          postBuild =
            let
              postBuildXkb = lib.replaceStrings
                [ "--with-xkb-bin-directory=${pkgs.xkbcomp}/bin \\" ]
                [ "--with-xkb-bin-directory= \\" ]
                (old.postBuild or "");
            in
            # Keep runtime DRI paths enabled so hosts with proper GPU userspace
            # + kernel device exposure can attempt hardware-backed GL.
            lib.replaceStrings
              [ "--disable-dri --disable-dri2 --disable-dri3 --enable-glx \\" ]
              [ "--enable-dri --enable-dri2 --enable-dri3 --enable-glx \\" ]
              postBuildXkb;
        });
        runtimeFfmpegRecorder = pkgs.ffmpeg-full.override {
          # Start from an empty feature profile and only enable what recording
          # from X11 needs.
          ffmpegVariant = "headless";
          withHeadlessDeps = false;
          withSmallDeps = false;
          withFullDeps = false;

          # Keep the CLI + core libraries required for x11grab capture and
          # mp4 output.
          buildFfmpeg = true;
          buildFfprobe = false;
          buildFfplay = false;
          buildQtFaststart = false;
          buildAvcodec = true;
          buildAvdevice = true;
          buildAvfilter = true;
          buildAvformat = true;
          buildAvutil = true;
          buildSwresample = true;
          buildSwscale = true;
          withBin = true;
          withLib = true;

          # X11 capture support.
          withXlib = true;
          withXcb = true;
          withXcbShm = true;
          withXcbShape = true;
          withXcbxfixes = true;

          # Trim optional integrations/codecs.
          withGPL = true;
          withVersion3 = false;
          withUnfree = false;
          withNetwork = false;
          withPulse = false;
          withSamba = false;
          withSdl2 = false;
          withAlsa = false;
          withGnutls = false;
          withSsh = false;
          withSrt = false;
          withOpencl = false;
          withOpengl = false;
          withVaapi = false;
          withVdpau = false;
          withVulkan = false;
          withAom = false;
          withDav1d = false;
          withOpenjpeg = false;
          withOpenmpt = false;
          withOpus = false;
          withSvtav1 = false;
          withTheora = false;
          withVorbis = false;
          withVpx = false;
          withWebp = false;
          withX264 = true;
          withX265 = false;
          withXvid = false;
          withMp3lame = false;
          withLzma = false;
          withBzlib = false;
          withXml2 = false;
          withZlib = true;
          withPixelutils = true;

          # No docs/man pages; optimize for size.
          withDocumentation = false;
          withHtmlDoc = false;
          withManPages = false;
          withPodDoc = false;
          withTxtDoc = false;
          withDoc = false;
          withSmallBuild = true;
          withStripping = true;
        };

        # Runtime components shared by all profiles.
        # requiredStatic = false means the component is optional in the strict-static profile.
        # includeInStatic = false excludes the component entirely from strict-static outputs.
        componentDefs = [
          {
            name = "tigervnc";
            attrPath = [ "tigervnc" ];
            package = runtimeTigerVNC;
            requiredStatic = true;
            staticPackage = staticTigerVNC;
          }
          {
            name = "xkbcomp";
            attrPaths = [
              [ "xkbcomp" ]
              [ "xorg" "xkbcomp" ]
            ];
            requiredStatic = false;
            includeInStatic = false;
          }
          {
            name = "xkeyboard-config";
            attrPath = [ "xkeyboard_config" ];
            requiredStatic = false;
            includeInStatic = false;
          }
          {
            name = "openbox";
            attrPath = [ "openbox" ];
            requiredStatic = false;
            includeInStatic = false;
          }
          {
            name = "fontconfig";
            package = pkgs.fontconfig.out;
            requiredStatic = false;
            includeInStatic = false;
          }
          {
            name = "xsetroot";
            attrPaths = [
              [ "xsetroot" ]
              [ "xorg" "xsetroot" ]
            ];
            requiredStatic = false;
            includeInStatic = false;
          }
          {
            name = "xdotool";
            attrPath = [ "xdotool" ];
            requiredStatic = false;
            includeInStatic = false;
          }
          {
            name = "ffmpeg";
            package = runtimeFfmpegRecorder;
            requiredStatic = false;
            includeInStatic = false;
          }
          {
            # Ensure the ffmpeg shared libraries in the runtime payload come
            # from the minimal recorder build, even if other closure members
            # provide a different libav* set.
            name = "ffmpeg-lib";
            package = runtimeFfmpegRecorder.lib;
            requiredStatic = false;
            includeInStatic = false;
          }
        ];

        includeComponentInStatic = def: def.includeInStatic or true;
        staticComponentDefs = lib.filter includeComponentInStatic componentDefs;

        componentAttrPaths = def: def.attrPaths or [ def.attrPath ];

        componentAttrPathLabels = def:
          map (attrPath: lib.concatStringsSep "." attrPath) (componentAttrPaths def);

        findFirstAttrPath = packageSet: attrPaths:
          lib.findFirst
            (attrPath: lib.hasAttrByPath attrPath packageSet)
            null
            attrPaths;

        componentPackage = packageSet: def:
          if def ? package then
            def.package
          else
          let
            resolvedAttrPath = findFirstAttrPath packageSet (componentAttrPaths def);
          in
          if resolvedAttrPath == null then
            throw "runtime component is unavailable: ${def.name} (tried: ${lib.concatStringsSep ", " (componentAttrPathLabels def)})"
          else
            lib.getAttrFromPath resolvedAttrPath packageSet;

        mkComponentFromPackage = def: pkg: {
          inherit (def) name;
          package = pkg;
          version = pkg.version or "unknown";
        };

        runtimeComponentsHostGlibc = map
          (def: mkComponentFromPackage def (componentPackage pkgs def))
          componentDefs;
        runtimeComponentsGlibc = runtimeComponentsHostGlibc ++ [
          (mkComponentFromPackage
            { name = "glibc"; }
            pkgs.glibc)
        ];
        runtimeComponentPackagesGlibc = map (component: component.package) runtimeComponentsGlibc;
        runtimeClosureInfoGlibc = pkgs.closureInfo {
          rootPaths = runtimeComponentPackagesGlibc;
        };

        tryEvaluatedPackage = candidate:
          let
            attempt = builtins.tryEval (builtins.seq candidate.drvPath candidate);
          in
          if attempt.success then
            attempt.value
          else
            null;

        findFirstEvaluatedPackageAtAttrPaths = packageSet: attrPaths:
          if attrPaths == [ ] then
            null
          else
            let
              attrPath = builtins.head attrPaths;
              remainingAttrPaths = builtins.tail attrPaths;
              candidate =
                if lib.hasAttrByPath attrPath packageSet then
                  tryEvaluatedPackage (lib.getAttrFromPath attrPath packageSet)
                else
                  null;
            in
            if candidate != null then
              candidate
            else
              findFirstEvaluatedPackageAtAttrPaths packageSet remainingAttrPaths;

        staticPackageFor = def:
          let
            requiredStatic = def.requiredStatic or true;
            staticOverridePackage =
              if def ? staticPackage then
                tryEvaluatedPackage def.staticPackage
              else
                null;

            pkg =
              if staticOverridePackage != null then
                staticOverridePackage
              else
                findFirstEvaluatedPackageAtAttrPaths
                  pkgsForStaticResolution.pkgsStatic
                  (componentAttrPaths def);
          in
          if pkg != null then
            pkg
          else if !requiredStatic then
            null
          else
            throw "required static runtime component is unavailable: ${def.name} (tried: ${lib.concatStringsSep ", " (componentAttrPathLabels def)}); use .#build for the primary bundled-glibc profile";

        runtimeComponentsStatic = lib.filter (component: component != null) (map
          (def:
            let
              pkg = staticPackageFor def;
            in
            if pkg == null then
              null
            else
              mkComponentFromPackage def pkg)
          staticComponentDefs);

        mkPortableRuntimeRoot = { profileName, components, closureInfo ? null }:
          pkgs.runCommand "portabledesktop-runtime-root-${profileName}"
            {
              nativeBuildInputs = [
                pkgs.coreutils
                pkgs.findutils
              ];
              closureInfoPath = if closureInfo == null then "" else "${closureInfo}";
            } ''
            set -euo pipefail

            mkdir -p "$out"
            copy_path_contents() {
              source_path="$1"
              chmod -R u+w "$out"

              if [ -d "$source_path" ]; then
                while IFS= read -r source_rel_path; do
                  source_rel_path="''${source_rel_path#./}"
                  destination_path="$out/$source_rel_path"

                  if [ ! -e "$destination_path" ] && [ ! -L "$destination_path" ]; then
                    continue
                  fi

                  source_entry_path="$source_path/$source_rel_path"
                  source_is_dir=0
                  if [ -d "$source_entry_path" ] && [ ! -L "$source_entry_path" ]; then
                    source_is_dir=1
                  fi

                  destination_is_dir=0
                  if [ -d "$destination_path" ] && [ ! -L "$destination_path" ]; then
                    destination_is_dir=1
                  fi

                  if [ "$source_is_dir" -ne "$destination_is_dir" ]; then
                    rm -rf "$destination_path"
                  fi
                done < <(cd "$source_path" && find . -mindepth 1 -print)

                cp_errors="$(mktemp)"
                if ! cp -a --remove-destination --no-preserve=ownership "$source_path"/. "$out"/ 2>"$cp_errors"; then
                  unexpected_copy_error=0
                  while IFS= read -r cp_error_line; do
                    case "$cp_error_line" in
                      "cp: cannot stat '"*"': No such file or directory")
                        missing_path="''${cp_error_line#cp: cannot stat \'}"
                        missing_path="''${missing_path%\': No such file or directory}"
                        if [ ! -L "$missing_path" ]; then
                          echo "$cp_error_line" >&2
                          unexpected_copy_error=1
                        fi
                        ;;
                      *)
                        echo "$cp_error_line" >&2
                        unexpected_copy_error=1
                        ;;
                    esac
                  done < "$cp_errors"
                  rm -f "$cp_errors"

                  if [ "$unexpected_copy_error" -ne 0 ]; then
                    echo "error: failed copying runtime path contents from $source_path" >&2
                    exit 1
                  fi
                else
                  rm -f "$cp_errors"
                fi
              else
                cp -a --remove-destination --no-preserve=ownership "$source_path" "$out"/
              fi
            }

            if [ -n "$closureInfoPath" ]; then
              closure_store_paths="$closureInfoPath/store-paths"
              if [ ! -f "$closure_store_paths" ]; then
                echo "error: closure info missing store-paths file at $closure_store_paths" >&2
                exit 1
              fi

              while IFS= read -r closure_path; do
                [ -n "$closure_path" ] || continue
                if [ ! -e "$closure_path" ]; then
                  echo "error: closure store path does not exist: $closure_path" >&2
                  exit 1
                fi

                copy_path_contents "$closure_path"
              done < "$closure_store_paths"
            fi

            component_paths=(${lib.escapeShellArgs (map (component: "${component.package}") components)})

            for component_path in "''${component_paths[@]}"; do
              copy_path_contents "$component_path"
            done

            while IFS= read -r link_path; do
              link_target="$(readlink "$link_path")"
              case "$link_target" in
                /nix/store/*/*)
                  inner_path="''${link_target#/nix/store/}"
                  inner_path="''${inner_path#*/}"
                  if [ -n "$inner_path" ] && [ -e "$out/$inner_path" ]; then
                    link_dir="$(dirname "$link_path")"
                    rewritten_target="$(realpath --relative-to="$link_dir" "$out/$inner_path")"
                    ln -snf "$rewritten_target" "$link_path"
                  fi
                  ;;
              esac
            done < <(find "$out" -type l -print)

            while IFS= read -r link_path; do
              link_target="$(readlink "$link_path")"
              case "$link_target" in
                /nix/store/*)
                  echo "error: runtime root contains /nix/store symlink: $link_path -> $link_target" >&2
                  exit 1
                  ;;
              esac
                done < <(find "$out" -type l -print)
            '';

        mkHostRuntimePatchConfig = targetSystem:
          if targetSystem == "x86_64-linux" then
            {
              dynamicLinker = "/lib64/ld-linux-x86-64.so.2";
              librarySearchPath = "$ORIGIN:$ORIGIN/../lib:$ORIGIN/../lib64:/lib:/lib64:/usr/lib:/usr/lib64:/usr/local/lib:/usr/local/lib64:/lib/x86_64-linux-gnu:/usr/lib/x86_64-linux-gnu";
            }
          else if targetSystem == "aarch64-linux" then
            {
              dynamicLinker = "/lib/ld-linux-aarch64.so.1";
              librarySearchPath = "$ORIGIN:$ORIGIN/../lib:$ORIGIN/../lib64:/lib:/lib64:/usr/lib:/usr/lib64:/usr/local/lib:/usr/local/lib64:/lib/aarch64-linux-gnu:/usr/lib/aarch64-linux-gnu";
            }
          else
            throw "runtime patchelf mode does not yet define host linker settings for ${targetSystem}";

        mkRuntimeTarball = {
          runtimeRoot,
          runtimeComponents,
          profileName ? "glibc",
          enforceStaticNoInterp ? false,
          patchRuntimeElf ? false
        }:
          let
            tarballName = "runtime-${system}-${profileName}.tar.zst";
            manifestName = "runtime-${system}-${profileName}.manifest.json";
            hostRuntimePatchConfig =
              if patchRuntimeElf then
                mkHostRuntimePatchConfig system
              else
                null;
            hostDynamicLinker =
              if hostRuntimePatchConfig == null then
                ""
              else
                hostRuntimePatchConfig.dynamicLinker;
            hostLibrarySearchPath =
              if hostRuntimePatchConfig == null then
                ""
              else
                hostRuntimePatchConfig.librarySearchPath;

            componentsJson = builtins.toJSON (map
              (component: {
                name = component.name;
                version = component.version;
              })
              runtimeComponents);
            xvncWrapperScript = pkgs.writeTextFile {
              name = "portabledesktop-runtime-Xvnc-wrapper";
              executable = true;
              text = ''
                #!/usr/bin/env bash
                set -euo pipefail

                script_dir="$(cd "$(dirname "''${BASH_SOURCE[0]}")" && pwd)"
                runtime_root="$(cd "$script_dir/.." && pwd)"
                xvnc_bin="$runtime_root/bin/Xvnc.real"
                export PORTABLEDESKTOP_RUNTIME_ROOT="$runtime_root"

                if [ ! -x "$xvnc_bin" ]; then
                  echo "error: missing Xvnc payload binary: $xvnc_bin" >&2
                  exit 1
                fi

                if [ -z "''${XKB_CONFIG_ROOT:-}" ] && [ -d "$runtime_root/share/X11/xkb" ]; then
                  export XKB_CONFIG_ROOT="$runtime_root/share/X11/xkb"
                fi
                if [ -z "''${XKB_BINDIR:-}" ] && [ -x "$runtime_root/bin/xkbcomp" ]; then
                  export XKB_BINDIR="$runtime_root/bin"
                fi

                xkb_args=()
                if [ -d "$runtime_root/share/X11/xkb" ]; then
                  xkb_args=(-xkbdir "$runtime_root/share/X11/xkb")
                fi

                runtime_library_path="$runtime_root/lib:$runtime_root/lib64:$runtime_root/usr/lib:$runtime_root/usr/lib64"

                if [ -n "''${PATH:-}" ]; then
                  export PATH="$runtime_root/bin:''${PATH}"
                else
                  export PATH="$runtime_root/bin"
                fi

                loader_path=""
                for candidate in \
                  "$runtime_root/lib64/ld-linux-x86-64.so.2" \
                  "$runtime_root/lib/ld-linux-x86-64.so.2" \
                  "$runtime_root/lib/ld-linux-aarch64.so.1" \
                  "$runtime_root/lib64/ld-linux-aarch64.so.1"; do
                  if [ -f "$candidate" ]; then
                    loader_path="$candidate"
                    break
                  fi
                done

                if [ -n "$loader_path" ]; then
                  if [ -f "$runtime_root/lib/libshaderc_shared.so.1" ]; then
                    exec "$loader_path" --preload "$runtime_root/lib/libshaderc_shared.so.1" --library-path "$runtime_library_path" "$xvnc_bin" "''${xkb_args[@]}" "$@"
                  fi
                  exec "$loader_path" --library-path "$runtime_library_path" "$xvnc_bin" "''${xkb_args[@]}" "$@"
                fi

                if [ -n "''${LD_LIBRARY_PATH:-}" ]; then
                  export LD_LIBRARY_PATH="$runtime_library_path:''${LD_LIBRARY_PATH}"
                else
                  export LD_LIBRARY_PATH="$runtime_library_path"
                fi

                exec "$xvnc_bin" "''${xkb_args[@]}" "$@"
              '';
            };
            openboxWrapperScript = pkgs.writeTextFile {
              name = "portabledesktop-runtime-openbox-wrapper";
              executable = true;
              text = ''
                #!/usr/bin/env bash
                set -euo pipefail

                script_dir="$(cd "$(dirname "''${BASH_SOURCE[0]}")" && pwd)"
                runtime_root="$(cd "$script_dir/.." && pwd)"
                runtime_library_path="$runtime_root/lib:$runtime_root/lib64:$runtime_root/usr/lib:$runtime_root/usr/lib64"
                openbox_real="$script_dir/openbox.real"

                if [ ! -x "$openbox_real" ]; then
                  echo "error: missing openbox payload binary: $openbox_real" >&2
                  exit 1
                fi

                if [ -d "$runtime_root/share" ]; then
                  if [ -n "''${XDG_DATA_DIRS:-}" ]; then
                    export XDG_DATA_DIRS="$runtime_root/share:''${XDG_DATA_DIRS}"
                  else
                    export XDG_DATA_DIRS="$runtime_root/share"
                  fi
                fi

                if [ -d "$runtime_root/etc/xdg" ]; then
                  if [ -n "''${XDG_CONFIG_DIRS:-}" ]; then
                    export XDG_CONFIG_DIRS="$runtime_root/etc/xdg:''${XDG_CONFIG_DIRS}"
                  else
                    export XDG_CONFIG_DIRS="$runtime_root/etc/xdg"
                  fi
                fi

                if [ -f "$runtime_root/etc/fonts/fonts.conf" ]; then
                  export FONTCONFIG_FILE="$runtime_root/etc/fonts/fonts.conf"
                fi
                if [ -d "$runtime_root/etc/fonts" ]; then
                  export FONTCONFIG_PATH="$runtime_root/etc/fonts"
                fi

                loader_path=""
                for candidate in \
                  "$runtime_root/lib64/ld-linux-x86-64.so.2" \
                  "$runtime_root/lib/ld-linux-x86-64.so.2" \
                  "$runtime_root/lib/ld-linux-aarch64.so.1" \
                  "$runtime_root/lib64/ld-linux-aarch64.so.1"; do
                  if [ -f "$candidate" ]; then
                    loader_path="$candidate"
                    break
                  fi
                done

                if [ -n "$loader_path" ]; then
                  exec "$loader_path" --library-path "$runtime_library_path" "$openbox_real" "$@"
                fi

                if [ -n "''${LD_LIBRARY_PATH:-}" ]; then
                  export LD_LIBRARY_PATH="$runtime_library_path:''${LD_LIBRARY_PATH}"
                else
                  export LD_LIBRARY_PATH="$runtime_library_path"
                fi

                exec "$openbox_real" "$@"
              '';
            };
            xkbcompWrapperScript = pkgs.writeTextFile {
              name = "portabledesktop-xkbcomp-wrapper";
              executable = true;
              text = ''
                #!/usr/bin/env bash
                set -euo pipefail

                script_dir="$(cd "$(dirname "''${BASH_SOURCE[0]}")" && pwd)"
                runtime_root="$(cd "$script_dir/.." && pwd)"
                runtime_library_path="$runtime_root/lib:$runtime_root/lib64:$runtime_root/usr/lib:$runtime_root/usr/lib64"
                xkbcomp_real="$script_dir/xkbcomp.real"

                if [ ! -x "$xkbcomp_real" ]; then
                  echo "error: missing xkbcomp payload binary: $xkbcomp_real" >&2
                  exit 1
                fi

                loader_path=""
                for candidate in \
                  "$runtime_root/lib64/ld-linux-x86-64.so.2" \
                  "$runtime_root/lib/ld-linux-x86-64.so.2" \
                  "$runtime_root/lib/ld-linux-aarch64.so.1" \
                  "$runtime_root/lib64/ld-linux-aarch64.so.1"; do
                  if [ -f "$candidate" ]; then
                    loader_path="$candidate"
                    break
                  fi
                done

                if [ -n "$loader_path" ]; then
                  exec "$loader_path" --library-path "$runtime_library_path" "$xkbcomp_real" "$@"
                fi

                if [ -n "''${LD_LIBRARY_PATH:-}" ]; then
                  export LD_LIBRARY_PATH="$runtime_library_path:''${LD_LIBRARY_PATH}"
                else
                  export LD_LIBRARY_PATH="$runtime_library_path"
                fi

                exec "$xkbcomp_real" "$@"
              '';
            };
            elfToolWrapperScript = pkgs.writeTextFile {
              name = "portabledesktop-runtime-elf-tool-wrapper";
              executable = true;
              text = ''
                #!/usr/bin/env bash
                set -euo pipefail

                script_dir="$(cd "$(dirname "''${BASH_SOURCE[0]}")" && pwd)"
                runtime_root="$(cd "$script_dir/.." && pwd)"
                runtime_library_path="$runtime_root/lib:$runtime_root/lib64:$runtime_root/usr/lib:$runtime_root/usr/lib64"
                tool_name="$(basename "$0")"
                tool_real="$script_dir/$tool_name.real"

                if [ ! -x "$tool_real" ]; then
                  echo "error: missing tool payload binary: $tool_real" >&2
                  exit 1
                fi

                loader_path=""
                for candidate in \
                  "$runtime_root/lib64/ld-linux-x86-64.so.2" \
                  "$runtime_root/lib/ld-linux-x86-64.so.2" \
                  "$runtime_root/lib/ld-linux-aarch64.so.1" \
                  "$runtime_root/lib64/ld-linux-aarch64.so.1"; do
                  if [ -f "$candidate" ]; then
                    loader_path="$candidate"
                    break
                  fi
                done

                if [ -n "$loader_path" ]; then
                  exec "$loader_path" --library-path "$runtime_library_path" "$tool_real" "$@"
                fi

                if [ -n "''${LD_LIBRARY_PATH:-}" ]; then
                  export LD_LIBRARY_PATH="$runtime_library_path:''${LD_LIBRARY_PATH}"
                else
                  export LD_LIBRARY_PATH="$runtime_library_path"
                fi

                exec "$tool_real" "$@"
              '';
            };
          in
          pkgs.stdenvNoCC.mkDerivation {
            pname = "portabledesktop-runtime-tarball-${profileName}";
            version = "1";
            dontUnpack = true;

            nativeBuildInputs = [
              pkgs.binutils
              pkgs.coreutils
              pkgs.findutils
              pkgs.gnugrep
              pkgs.gnutar
              pkgs.zstd
            ] ++ lib.optional patchRuntimeElf pkgs.patchelf;

            installPhase = ''
              runHook preInstall

              mkdir -p "$out"
              runtimePayloadRoot="${runtimeRoot}"
              runtimePayloadCopyRoot=""

              cleanup_runtime_payload_copy() {
                if [ -n "$runtimePayloadCopyRoot" ] && [ -d "$runtimePayloadCopyRoot" ]; then
                  chmod -R u+w "$runtimePayloadCopyRoot" || true
                  rm -rf "$runtimePayloadCopyRoot"
                fi
              }
              trap cleanup_runtime_payload_copy EXIT

              ensure_runtime_payload_copy() {
                if [ -n "$runtimePayloadCopyRoot" ] && [ -d "$runtimePayloadCopyRoot" ]; then
                  return
                fi

                runtimePayloadCopyRoot="$(mktemp -d)"
                cp -a --remove-destination --no-preserve=ownership "$runtimePayloadRoot"/. "$runtimePayloadCopyRoot"/
                chmod -R u+w "$runtimePayloadCopyRoot"
                runtimePayloadRoot="$runtimePayloadCopyRoot"
              }


              while IFS= read -r link_path; do
                link_target="$(readlink "$link_path")"
                case "$link_target" in
                  /nix/store/*)
                    echo "error: runtime root includes /nix/store symlink: $link_path -> $link_target" >&2
                    exit 1
                    ;;
                esac
              done < <(find "${runtimeRoot}" -type l -print)

              if [ "${if patchRuntimeElf then "1" else "0"}" = "1" ]; then
                ensure_runtime_payload_copy

                runtimeRunpath='${hostLibrarySearchPath}'
                hostInterpreter='${hostDynamicLinker}'

                elf_dynamic_patched=0
                elf_interp_patched=0

                while IFS= read -r candidate_path; do
                  if ! readelf -h "$candidate_path" >/dev/null 2>&1; then
                    continue
                  fi

                  if ! readelf -l "$candidate_path" | grep -Eq '(^|[[:space:]])DYNAMIC([[:space:]]|$)'; then
                    continue
                  fi

                  patchelf --set-rpath "$runtimeRunpath" "$candidate_path"
                  elf_dynamic_patched=$((elf_dynamic_patched + 1))

                  if readelf -l "$candidate_path" | grep -q "Requesting program interpreter"; then
                    patchelf --set-interpreter "$hostInterpreter" "$candidate_path"
                    elf_interp_patched=$((elf_interp_patched + 1))
                  fi
                done < <(find "$runtimePayloadRoot" -type f -print)

                echo "patched dynamic ELF files: $elf_dynamic_patched (set interpreter on $elf_interp_patched)" >&2
                echo "patched interpreter: $hostInterpreter" >&2
                echo "patched runpath: $runtimeRunpath" >&2
              fi

              if [ "${if enforceStaticNoInterp then "1" else "0"}" = "1" ]; then
                if [ ! -d "$runtimePayloadRoot/bin" ]; then
                  echo "error: static runtime enforcement expected a bin directory at $runtimePayloadRoot/bin" >&2
                  exit 1
                fi

                elf_checked=0
                while IFS= read -r exe_path; do
                  if ! readelf -h "$exe_path" >/dev/null 2>&1; then
                    continue
                  fi

                  elf_checked=$((elf_checked + 1))
                  if readelf -l "$exe_path" | grep -q "Requesting program interpreter"; then
                    echo "error: static runtime contains dynamically linked ELF executable: $exe_path" >&2
                    readelf -l "$exe_path" | grep "Requesting program interpreter" >&2 || true
                    exit 1
                  fi
                done < <(find "$runtimePayloadRoot/bin" -type f -perm -0100 -print)

                echo "static ELF enforcement passed: checked $elf_checked executable(s) under $runtimePayloadRoot/bin" >&2
              fi

              # Minimize payload to server-only runtime essentials.
              ensure_runtime_payload_copy

              if [ -d "$runtimePayloadRoot/bin" ]; then
                find "$runtimePayloadRoot/bin" -mindepth 1 -maxdepth 1 \
                  \( -type f -o -type l \) \
                  ! -name 'Xvnc' \
                  ! -name 'xkbcomp' \
                  ! -name 'openbox' \
                  ! -name 'xsetroot' \
                  ! -name 'xdotool' \
                  ! -name 'ffmpeg' \
                  ! -name '.openbox-wrapped' \
                  -delete
              fi

              rm -rf \
                "$runtimePayloadRoot/sbin" \
                "$runtimePayloadRoot/libexec" \
                "$runtimePayloadRoot/include" \
                "$runtimePayloadRoot/example" \
                "$runtimePayloadRoot/x86_64-unknown-linux-gnu"

              if [ -d "$runtimePayloadRoot/share" ]; then
                find "$runtimePayloadRoot/share" -mindepth 1 -maxdepth 1 \
                  ! -name 'X11' \
                  ! -name 'fontconfig' \
                  ! -name 'fonts' \
                  ! -name 'openbox' \
                  ! -name 'themes' \
                  ! -name 'xkeyboard-config-2' \
                  -exec rm -rf {} +
              fi

              if [ -f "$runtimePayloadRoot/etc/fonts/fonts.conf" ]; then
                # Replace build-time /nix/store font dirs with the bundled runtime font dir.
                sed -i -E '/<dir>\/nix\/store\/[^<]*<\/dir>/d' "$runtimePayloadRoot/etc/fonts/fonts.conf"
                sed -i -E '/<dir[^>]*>.*share\/fonts<\/dir>/d' "$runtimePayloadRoot/etc/fonts/fonts.conf"
                if ! grep -q '<dir prefix="relative">../../share/fonts</dir>' "$runtimePayloadRoot/etc/fonts/fonts.conf"; then
                  sed -i 's#</fontconfig>#<dir prefix="relative">../../share/fonts</dir>\n</fontconfig>#' "$runtimePayloadRoot/etc/fonts/fonts.conf"
                fi
              fi

              if [ -d "$runtimePayloadRoot/share/X11" ]; then
                find "$runtimePayloadRoot/share/X11" -mindepth 1 -maxdepth 1 \
                  ! -name 'xkb' \
                  -exec rm -rf {} +
              fi

              xkbRoot="$runtimePayloadRoot/share/xkeyboard-config-2"
              if [ -d "$xkbRoot" ]; then
                # Keep only the XKB data required for clean default keymap init.
                find "$xkbRoot" -type f -name 'README*' -delete || true

                if [ -d "$xkbRoot/compat" ]; then
                  find "$xkbRoot/compat" -type f \
                    ! -name 'accessx' \
                    ! -name 'basic' \
                    ! -name 'caps' \
                    ! -name 'complete' \
                    ! -name 'iso9995' \
                    ! -name 'ledcaps' \
                    ! -name 'lednum' \
                    ! -name 'ledscroll' \
                    ! -name 'level5' \
                    ! -name 'misc' \
                    ! -name 'mousekeys' \
                    ! -name 'xfree86' \
                    -delete
                fi

                if [ -d "$xkbRoot/types" ]; then
                  find "$xkbRoot/types" -type f \
                    ! -name 'basic' \
                    ! -name 'complete' \
                    ! -name 'extra' \
                    ! -name 'iso9995' \
                    ! -name 'level5' \
                    ! -name 'mousekeys' \
                    ! -name 'numpad' \
                    ! -name 'pc' \
                    -delete
                fi

                if [ -d "$xkbRoot/keycodes" ]; then
                  find "$xkbRoot/keycodes" -mindepth 1 -type d -exec rm -rf {} +
                  find "$xkbRoot/keycodes" -type f \
                    ! -name 'aliases' \
                    ! -name 'evdev' \
                    -delete
                fi

                if [ -d "$xkbRoot/rules" ]; then
                  find "$xkbRoot/rules" -type f \
                    ! -name 'evdev' \
                    -delete
                fi

                if [ -d "$xkbRoot/geometry" ]; then
                  find "$xkbRoot/geometry" -mindepth 1 -type d -exec rm -rf {} +
                  find "$xkbRoot/geometry" -type f \
                    ! -name 'pc' \
                    -delete
                fi

                if [ -d "$xkbRoot/symbols" ]; then
                  find "$xkbRoot/symbols" -mindepth 1 -type d -exec rm -rf {} +
                  find "$xkbRoot/symbols" -maxdepth 1 -type f \
                    ! -name 'inet' \
                    ! -name 'keypad' \
                    ! -name 'pc' \
                    ! -name 'srvr_ctrl' \
                    ! -name 'us' \
                    -delete
                fi

                find "$xkbRoot" -type d -empty -delete || true
              fi

              if [ -d "$runtimePayloadRoot/share/themes" ]; then
                # Keep only the default Openbox theme used by etc/xdg/openbox/rc.xml.
                find "$runtimePayloadRoot/share/themes" -mindepth 1 -maxdepth 1 \
                  ! -name 'Clearlooks' \
                  -exec rm -rf {} +
                if [ -d "$runtimePayloadRoot/share/themes/Clearlooks" ]; then
                  find "$runtimePayloadRoot/share/themes/Clearlooks" -mindepth 1 -maxdepth 1 \
                    ! -name 'openbox-3' \
                    -exec rm -rf {} +
                fi
              fi

              # ffmpeg closure carries optional WildMIDI/Freepats sample banks and
              # AJA SDK source/header trees that are not required for desktop capture.
              rm -rf \
                "$runtimePayloadRoot/Drum_000" \
                "$runtimePayloadRoot/Tone_000" \
                "$runtimePayloadRoot/libajantv2"

              rm -f \
                "$runtimePayloadRoot/COPYING" \
                "$runtimePayloadRoot/README" \
                "$runtimePayloadRoot/crude.cfg" \
                "$runtimePayloadRoot/freepats.cfg"

              find "$runtimePayloadRoot" -mindepth 1 -maxdepth 1 -type f \
                -name '*-wildmidi.cfg' \
                -delete

              for lib_root in \
                "$runtimePayloadRoot/lib" \
                "$runtimePayloadRoot/lib64" \
                "$runtimePayloadRoot/usr/lib" \
                "$runtimePayloadRoot/usr/lib64"; do
                [ -d "$lib_root" ] || continue
                rm -rf "$lib_root/pkgconfig" "$lib_root/cmake"
                find "$lib_root" -mindepth 1 -maxdepth 1 -type d \
                  \( \
                    -name 'python*' -o \
                    -name 'perl*' -o \
                    -name 'ruby*' -o \
                    -name 'gcc' -o \
                    -name 'girepository-1.0' -o \
                    -name 'gstreamer-1.0' -o \
                    -name 'pipewire-0.3' -o \
                    -name 'spa-0.2' -o \
                    -name 'udev' -o \
                    -name 'systemd' -o \
                    -name 'security' -o \
                    -name 'locale' -o \
                    -name 'gconv' -o \
                    -name 'krb5' -o \
                    -name 'pkcs11' \
                  \) \
                  -exec rm -rf {} +
              done

              find "$runtimePayloadRoot" -type f \
                \( \
                  -name '*.a' -o \
                  -name '*.la' -o \
                  -name '*.pc' \
                \) \
                -delete

              runtimeLibDirs=()
              for lib_dir_candidate in \
                "$runtimePayloadRoot/lib" \
                "$runtimePayloadRoot/lib64" \
                "$runtimePayloadRoot/usr/lib" \
                "$runtimePayloadRoot/usr/lib64"; do
                [ -d "$lib_dir_candidate" ] || continue
                runtimeLibDirs+=("$lib_dir_candidate")
              done

              declare -A runtimeKeepSet=()
              runtimeDepQueue=()

              queue_runtime_dep() {
                dep_path="$1"
                [ -n "$dep_path" ] || return
                case "$dep_path" in
                  "$runtimePayloadRoot"/*)
                    runtimeDepQueue+=("$dep_path")
                    ;;
                esac
              }

              queue_runtime_dep "$runtimePayloadRoot/bin/Xvnc"
              queue_runtime_dep "$runtimePayloadRoot/bin/xkbcomp"
              queue_runtime_dep "$runtimePayloadRoot/bin/openbox"
              queue_runtime_dep "$runtimePayloadRoot/bin/xsetroot"
              queue_runtime_dep "$runtimePayloadRoot/bin/xdotool"
              queue_runtime_dep "$runtimePayloadRoot/bin/ffmpeg"
              queue_runtime_dep "$runtimePayloadRoot/bin/.openbox-wrapped"

              if [ -d "$runtimePayloadRoot/lib/xorg/modules" ]; then
                while IFS= read -r module_elf; do
                  queue_runtime_dep "$module_elf"
                done < <(find "$runtimePayloadRoot/lib/xorg/modules" -type f -name '*.so*' -print)
              fi

              if [ "''${#runtimeLibDirs[@]}" -gt 0 ]; then
                while IFS= read -r loader_candidate; do
                  queue_runtime_dep "$loader_candidate"
                done < <(find "''${runtimeLibDirs[@]}" -type f -name 'ld-linux*.so*' -print 2>/dev/null)

              fi

              while [ "''${#runtimeDepQueue[@]}" -gt 0 ]; do
                dep_path="''${runtimeDepQueue[0]}"
                runtimeDepQueue=("''${runtimeDepQueue[@]:1}")

                [ -e "$dep_path" ] || continue
                if [ -n "''${runtimeKeepSet[$dep_path]:-}" ]; then
                  continue
                fi
                runtimeKeepSet["$dep_path"]=1

                if [ -L "$dep_path" ]; then
                  dep_real_path="$(readlink -f "$dep_path" 2>/dev/null || true)"
                  queue_runtime_dep "$dep_real_path"
                fi

                if ! readelf -h "$dep_path" >/dev/null 2>&1; then
                  continue
                fi

                while IFS= read -r needed_soname; do
                  [ -n "$needed_soname" ] || continue
                  if [ "''${#runtimeLibDirs[@]}" -gt 0 ]; then
                    while IFS= read -r needed_path; do
                      queue_runtime_dep "$needed_path"
                    done < <(find "''${runtimeLibDirs[@]}" -name "$needed_soname" \( -type f -o -type l \) -print 2>/dev/null)
                  fi
                done < <(readelf -d "$dep_path" 2>/dev/null | sed -nE 's/.*Shared library: \[([^]]+)\].*/\1/p')
              done

              removedSharedLibCount=0
              if [ "''${#runtimeLibDirs[@]}" -gt 0 ]; then
                while IFS= read -r candidate_lib; do
                  if [ -z "''${runtimeKeepSet[$candidate_lib]:-}" ]; then
                    rm -f "$candidate_lib"
                    removedSharedLibCount=$((removedSharedLibCount + 1))
                  fi
                done < <(find "''${runtimeLibDirs[@]}" -type f \( -name '*.so' -o -name '*.so.*' \) -print 2>/dev/null)

                find "''${runtimeLibDirs[@]}" -xtype l -delete || true

                # Remove known non-runtime trees that still survive ELF closure pruning.
                find "''${runtimeLibDirs[@]}" -mindepth 1 -maxdepth 1 -type d \
                  \( \
                    -name 'bash' -o \
                    -name 'cups' -o \
                    -name 'ckport' -o \
                    -name 'dbus-1.0' -o \
                    -name 'gio' -o \
                    -name 'glib-2.0' -o \
                    -name 'glibmm-2.4' -o \
                    -name 'giomm-2.4' -o \
                    -name 'graphene-1.0' -o \
                    -name 'libxml++-3.0' -o \
                    -name 'sigc++-2.0' -o \
                    -name 'libv4l' -o \
                    -name 'kexec-tools' -o \
                    -name 'modprobe.d' -o \
                    -name 'tmpfiles.d' -o \
                    -name 'sysusers.d' -o \
                    -name 'pcrlock.d' \
                  \) \
                  -exec rm -rf {} +

                find "''${runtimeLibDirs[@]}" -type f \
                  \( \
                    -name '*.o' -o \
                    -name '*.h' -o \
                    -name '*.hpp' -o \
                    -name '*.json' \
                  \) \
                  -delete || true

                find "''${runtimeLibDirs[@]}" -type d -empty -delete || true
              fi

              rm -rf \
                "$runtimePayloadRoot/nix-support" \
                "$runtimePayloadRoot/var" \
                "$runtimePayloadRoot/lib/libffado"

              rm -f \
                "$runtimePayloadRoot/root.ds" \
                "$runtimePayloadRoot/root.hints" \
                "$runtimePayloadRoot/root.key" \
                "$runtimePayloadRoot/etc/rpc" \
                "$runtimePayloadRoot/lib/"*.spec

              if [ -d "$runtimePayloadRoot/lib/X11" ]; then
                rm -rf "$runtimePayloadRoot/lib/X11/app-defaults"
              fi

              find "$runtimePayloadRoot/etc" -type d -empty -delete || true
              find "$runtimePayloadRoot" -depth -type d -empty -delete || true

              echo "runtime minimization: kept ''${#runtimeKeepSet[@]} dependency-path(s), removed $removedSharedLibCount shared library file(s)" >&2

              if [ -x "$runtimePayloadRoot/bin/xkbcomp" ]; then
                mv "$runtimePayloadRoot/bin/xkbcomp" "$runtimePayloadRoot/bin/xkbcomp.real"
                cp "${xkbcompWrapperScript}" "$runtimePayloadRoot/bin/xkbcomp"
                chmod 0755 "$runtimePayloadRoot/bin/xkbcomp"
              fi

              if [ -e "$runtimePayloadRoot/bin/Xvnc.real" ]; then
                echo "error: runtime payload unexpectedly already contains bin/Xvnc.real" >&2
                exit 1
              fi

              if [ -x "$runtimePayloadRoot/bin/Xvnc" ]; then
                mv "$runtimePayloadRoot/bin/Xvnc" "$runtimePayloadRoot/bin/Xvnc.real"
                cp "${xvncWrapperScript}" "$runtimePayloadRoot/bin/Xvnc"
                chmod 0755 "$runtimePayloadRoot/bin/Xvnc"
              fi

              if [ -e "$runtimePayloadRoot/bin/openbox.real" ]; then
                echo "error: runtime payload unexpectedly already contains bin/openbox.real" >&2
                exit 1
              fi

              openbox_real_source=""
              if [ -x "$runtimePayloadRoot/bin/.openbox-wrapped" ]; then
                openbox_real_source="$runtimePayloadRoot/bin/.openbox-wrapped"
              elif [ -x "$runtimePayloadRoot/bin/openbox" ]; then
                openbox_real_source="$runtimePayloadRoot/bin/openbox"
              fi

              if [ -n "$openbox_real_source" ]; then
                mv "$openbox_real_source" "$runtimePayloadRoot/bin/openbox.real"
                rm -f "$runtimePayloadRoot/bin/openbox" "$runtimePayloadRoot/bin/.openbox-wrapped"
                cp "${openboxWrapperScript}" "$runtimePayloadRoot/bin/openbox"
                chmod 0755 "$runtimePayloadRoot/bin/openbox"
              fi

              for tool_name in ffmpeg xsetroot xdotool; do
                tool_path="$runtimePayloadRoot/bin/$tool_name"
                tool_real_path="$runtimePayloadRoot/bin/$tool_name.real"

                if [ -x "$tool_path" ]; then
                  mv "$tool_path" "$tool_real_path"
                  cp "${elfToolWrapperScript}" "$tool_path"
                  chmod 0755 "$tool_path"
                fi
              done

              tar \
                --sort=name \
                --mtime='@1' \
                --owner=0 \
                --group=0 \
                --numeric-owner \
                -C "$runtimePayloadRoot" \
                -cf - . | zstd -T0 -19 -o "$out/${tarballName}"

              tarball_sha256="$(sha256sum "$out/${tarballName}" | cut -d ' ' -f1)"
              tarball_size="$(stat -c '%s' "$out/${tarballName}")"
              generated_at="$(date -u +'%Y-%m-%dT%H:%M:%SZ')"
              runtime_version="${profileName}-${system}-$(basename "${runtimeRoot}")"

              cat > "$out/${manifestName}" <<EOF
              {
                "version": "$runtime_version",
                "archive_sha256": "$tarball_sha256",
                "xvnc_path": "bin/Xvnc",
                "build": {
                  "schema_version": 1,
                  "profile": "${profileName}",
                  "system": "${system}",
                  "generated_at_utc": "$generated_at",
                  "runtime_root_store_path": "${runtimeRoot}",
                  "archive": {
                    "file": "${tarballName}",
                    "sha256": "$tarball_sha256",
                    "size_bytes": $tarball_size
                  },
                  "components": ${componentsJson}
                },
                "components": ${componentsJson}
              }
              EOF

              runHook postInstall
            '';

            passthru = {
              inherit manifestName tarballName profileName runtimeRoot;
            };

            meta = {
              description = "Compressed runtime tarball and manifest for PortableDesktop";
              platforms = lib.platforms.linux;
            };
          };

        runtimeRootGlibc = mkPortableRuntimeRoot {
          profileName = "glibc";
          components = runtimeComponentsGlibc;
          closureInfo = runtimeClosureInfoGlibc;
        };

        runtimeRootStatic = mkPortableRuntimeRoot {
          profileName = "static";
          components = runtimeComponentsStatic;
        };

        mkRuntimeTarballFor = {
          runtimeRoot,
          runtimeComponents,
          profileName,
          enforceStaticNoInterp ? false,
          patchRuntimeElf ? false
        }:
          mkRuntimeTarball {
            inherit runtimeRoot runtimeComponents profileName enforceStaticNoInterp patchRuntimeElf;
          };

        # Experimental tarball: strict static profile (no silent glibc substitution).
        runtimeTarballStatic = mkRuntimeTarballFor {
          runtimeRoot = runtimeRootStatic;
          runtimeComponents = runtimeComponentsStatic;
          profileName = "static";
          enforceStaticNoInterp = true;
        };

        runtimeTarballGlibc = mkRuntimeTarballFor {
          runtimeRoot = runtimeRootGlibc;
          runtimeComponents = runtimeComponentsGlibc;
          profileName = "glibc";
        };

        buildArtifact = pkgs.runCommand "portabledesktop-build"
          {
            nativeBuildInputs = [
              pkgs.coreutils
              pkgs.gnutar
              pkgs.jq
              pkgs.zstd
            ];
          } ''
          mkdir -p "$out/output"

          zstd -dc "${runtimeTarballGlibc}/${runtimeTarballGlibc.tarballName}" > "$out/output.tar"
          tar --delay-directory-restore -C "$out/output" -xf "$out/output.tar"

          archive_sha256="$(sha256sum "$out/output.tar" | awk '{print $1}')"
          archive_size_bytes="$(stat -c '%s' "$out/output.tar")"

          jq \
            --arg archive_sha256 "$archive_sha256" \
            --argjson archive_size_bytes "$archive_size_bytes" \
            '
              .archive_sha256 = $archive_sha256
              | .build.archive.file = "output.tar"
              | .build.archive.sha256 = $archive_sha256
              | .build.archive.size_bytes = $archive_size_bytes
            ' \
            "${runtimeTarballGlibc}/${runtimeTarballGlibc.manifestName}" \
            > "$out/manifest.json"
        '';
      in
      {
        packages = rec {
          build = buildArtifact;
          default = build;
        };
      });
}
