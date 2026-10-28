#!/bin/sh
# Do not source credentials or enable shell tracing.
set +x
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
repo_dir=$(dirname -- "$script_dir")
config_path=${OPCODE_LIVE_CONFIG:-${XDG_CONFIG_HOME:-${HOME}/.config}/opcode/integration.json}
action=help
explicit_config=${OPCODE_LIVE_CONFIG:+yes}
providers=${OPCODE_LIVE_PROVIDERS:-}
usage() {
    cat <<'HELP'
Usage:
  ./scripts/test-providers.sh --init [--config PATH]
  ./scripts/test-providers.sh --run [--config PATH] [--providers openai,anthropic,...]

--init creates a private configuration template without overwriting an existing file.
--run explicitly enables billable API integration tests. Configure model IDs and
credentials first; environment variables override the file. No API keys are
accepted as command-line arguments. See internal/llm/provider/integration/README.md.
HELP
}
while [ "$#" -gt 0 ]; do
    case "$1" in
        --init|--run)
            [ "$action" = help ] || { usage >&2; exit 2; }
            action=${1#--}; shift ;;
        --config)
            [ "$#" -ge 2 ] && [ -n "$2" ] || { usage >&2; exit 2; }
            config_path=$2; explicit_config=yes; shift 2 ;;
        --providers)
            [ "$#" -ge 2 ] && [ -n "$2" ] || { usage >&2; exit 2; }
            providers=$2; shift 2 ;;
        --help|-h) usage; exit 0 ;;
        *) usage >&2; exit 2 ;;
    esac
done
# Resolve a relative path before changing to the repository directory.
case "$config_path" in /*) ;; *) config_path=$PWD/$config_path ;; esac
case "$action" in
    help) usage ;;
    init)
        umask 077
        mkdir -p -- "$(dirname -- "$config_path")"
        if [ -e "$config_path" ] || [ -L "$config_path" ]; then
            printf '%s\n' 'Configuration already exists; no file was overwritten.' >&2
            exit 1
        fi
        if ! (set -C; cat "$repo_dir/examples/integration.example.json" > "$config_path") 2>/dev/null; then
            printf '%s\n' 'Configuration already exists or could not be created; no file was overwritten.' >&2
            exit 1
        fi
        printf 'Private template created: %s\n' "$config_path"
        printf '%s\n' 'Configure only the desired providers, set enabled=true and choose a model available to your account.'
        ;;
    run)
        export OPCODE_LIVE_TESTS=1
        export OPCODE_LIVE_PROVIDERS="$providers"
        # An explicit path must exist; the implicit default is optional in env-only mode.
        if [ -f "$config_path" ] || [ "$explicit_config" = yes ]; then
            export OPCODE_LIVE_CONFIG="$config_path"
        fi
        cd -- "$repo_dir"
        exec go test -tags=integration ./internal/llm/provider/integration -run '^TestLiveProviders$' -count=1 -v -timeout=30m
        ;;
esac
