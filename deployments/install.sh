#!/bin/bash

# --- Global settings -----------------------
GITHUB_REPO="PlushGuardian/3x-ui-exporter"
SOURCE_BRANCH="main"

# -------------------------------------------

GREEN='\033[1;32m'
PURPLE='\033[1;35m'
NC='\033[0m'

step() {
  echo -e "\n${GREEN}[$1/8] $2${NC}"
}


# ------------------------------------------------------------
# abort_on_error <error_message> [folder_to_remove]
#
# Must be called immediately after the command you want to guard.
# Arguments:
#   $1 (optional) – Error message to print on failure.
#   $2 (optional) – Directory to remove (with `rm -rf`) on failure.
#
# If the previous command failed (exit ≠ 0), the function:
#   1. Prints the provided error message to stderr, or a default one, if none was provided.
#   2. If $2 is given and not empty, deletes that directory.
#   3. Exits the script with the same non‑zero exit code.
# ------------------------------------------------------------
abort_on_error() {
    local last_exit=$?
    if [[ $last_exit -ne 0 ]]; then
        local error_message="${1:-}"
        if [[ -z $error_message ]]; then
            error_message="Something went wrong during installation, exiting."
        fi
        echo "$error_message" >&2
        if [[ -n "${2:-}" ]]; then
            echo "Cleaning up directory: $2" >&2
            rm -rf "$2"
        fi
        exit "$last_exit"
    fi
}

# ------------------------------------------------------------------
# prompt_input <var_name> <prompt_text> <default_val> [validation] [--secret]
#
# Prompts the user repeatedly until a valid answer is given.
#   validation  – optional: "number", "nonempty", "port", "bool", … or a custom function name.
#   --secret    – optional: hides typed characters (useful for passwords).
#
# When --secret is used, the prompt shows "[hidden]" instead of the default.
# Pressing Enter still accepts the default value (even if hidden).
# ------------------------------------------------------------------
prompt_input() {
    local var_name="$1"
    local prompt_text="$2"
    local default_val="$3"

    local validation=""
    local secret=0

    shift 3
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --secret|--password)
                secret=1
                shift
                ;;
            *)
                if [[ -z "$validation" ]]; then
                    validation="$1"
                else
                    echo "prompt_input: unexpected argument '$1'" >&2
                    return 1
                fi
                shift
                ;;
        esac
    done

    local input_valprompt_full
    if [[ -z "$default_val" ]]; then
        prompt_full="${prompt_text} (required): "
    else
        prompt_full="${prompt_text} [${default_val}]: "
    fi

    while true; do
        if [[ $secret -eq 1 ]]; then
            read -s -p "$prompt_full" input_val
            echo
        else
            read -p "$prompt_full" input_val
        fi

        if [[ -z "$default_val" && -z "$input_val" ]]; then
            echo "  This value is required." >&2
            continue
        fi
        input_val="${input_val:-$default_val}"

        # Validation logic
        case "$validation" in
            number)
                if [[ "$input_val" =~ ^[0-9]+$ ]]; then break
                else echo "  Please enter a positive integer." >&2; fi
                ;;
            nonempty)
                if [[ -n "$input_val" ]]; then break
                else echo "  This value cannot be empty." >&2; fi
                ;;
            port)
                if [[ "$input_val" =~ ^[0-9]+$ ]] && (( input_val >= 1 && input_val <= 65535 )); then break
                else echo "  Please enter a valid port (1-65535)." >&2; fi
                ;;
            bool|boolean)
                case "${input_val,,}" in
                    true|yes|1)   input_val="true";  break ;;
                    false|no|0)   input_val="false"; break ;;
                    *)            echo "  Please answer 'true' or 'false'." >&2 ;;
                esac
                ;;
            "") break ;;   # no validation → accept anything
            *)
                # Assume it's a custom function name
                if declare -F "$validation" &>/dev/null && "$validation" "$input_val"; then
                    break
                else
                    echo "  Invalid input. Please try again." >&2
                fi
                ;;
        esac
    done

    export "$var_name"="$input_val"
}

validate_ip() {
    local ip="$1"
    [[ "$ip" =~ ^[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}$ ]] || return 1
    IFS='.' read -r a b c d <<< "$ip"
    (( a <= 255 && b <= 255 && c <= 255 && d <= 255 ))
}

validate_duration() {
    [[ "$1" =~ ^[0-9]+(s|m|h)$ ]]
}


# ------------------------------------------------------------------
# ask_continue [question] [yes_msg] [no_msg]
# Returns 0 (true) on yes, 1 (false) on no.
# Prints the corresponding message if provided.
# ------------------------------------------------------------------
ask_continue() {
    local question="${1:-Do you want to continue?}"
    local yes_msg="${2:-}"   # message on yes (optional)
    local no_msg="${3:-}"    # message on no  (optional)
    local ans
    while true; do
        read -p "${question} (y/n): " ans
        case "${ans,,}" in
            y|yes)
                [[ -n "$yes_msg" ]] && echo "$yes_msg"
                return 0 ;;
            n|no)
                [[ -n "$no_msg"  ]] && echo "$no_msg"
                return 1 ;;
            *)
                echo "Please answer yes or no." ;;
        esac
    done
}

# ------------------------------------------------------------------
# substitute_template [template_file] [output_file]
# Replaces ${VARIABLE} placeholders with exported env vars.
# Works without external tools like envsubst
# ------------------------------------------------------------------
substitute_template() {
    local tmpl="$1"
    local out="$2"
    local line var

    while IFS= read -r line; do
        while [[ "$line" =~ \$\{([_a-zA-Z][_a-zA-Z0-9]*)\} ]]; do
            var="${BASH_REMATCH[1]}"
            line="${line//\$\{${var}\}/${!var}}"
        done
        printf '%s\n' "$line"
    done < "$tmpl" > "$out"
}



# Check if script is run as root
if [ "$(id -u)" -ne 0 ]; then
    echo "Error: This script must be run as root (sudo)."
    exit 1
fi

# Determine system architecture
ARCH=$(uname -m)
case ${ARCH} in
    x86_64)
        ARCH="amd64"
        ;;
    aarch64)
        ARCH="arm64"
        ;;
    *)
        echo "Unsupported architecture: ${ARCH}"
        exit 1
        ;;
esac

# Get latest release tag
echo "Fetching latest release information..."
LATEST_RELEASE=$(curl -s https://api.github.com/repos/${GITHUB_REPO}/releases/latest)
abort_on_error "Failed to fetch release information. Installation aborted."

VERSION=$(echo "${LATEST_RELEASE}" | grep -Po '"tag_name": "\K.*?(?=")')
echo -e "\n${PURPLE}✨ Starting 3X-UI Exporter $VERSION automated install wizard...\033[0m"

# Create dedicated system user for running the service
step 1 "Creating x-ui-exporter user"
if ! id -u x-ui-exporter > /dev/null 2>&1; then
    useradd -r -s /bin/false x-ui-exporter
    abort_on_error "Failed to create user. Installation aborted."
fi

# Download the appropriate archive
TEMP_DIR=$(mktemp -d)
ARCHIVE_NAME="x-ui-exporter-${VERSION}-linux-${ARCH}.tar.gz"
DOWNLOAD_URL="https://github.com/${GITHUB_REPO}/releases/download/${VERSION}/${ARCHIVE_NAME}"

step 2 "Downloading binary from: ${DOWNLOAD_URL}"
curl -L -o "${TEMP_DIR}/${ARCHIVE_NAME}" "${DOWNLOAD_URL}"
abort_on_error  "Failed to download binary. Installation aborted." "${TEMP_DIR}"

# Extract binary
step 3 "Extracting binary..."
tar -xzf "${TEMP_DIR}/${ARCHIVE_NAME}" -C "${TEMP_DIR}"
abort_on_error "Failed to extract binary. Installation aborted." "${TEMP_DIR}"

# Force remove old binary if exists
if [ -f /usr/local/bin/x-ui-exporter ]; then
    rm -f /usr/local/bin/x-ui-exporter
    abort_on_error "Failed to remove old binary. Installation aborted." "${TEMP_DIR}"
fi

# Install binary to /usr/local/bin
step 4 "Installing binary to /usr/local/bin..."
cp "${TEMP_DIR}/x-ui-exporter" /usr/local/bin/
abort_on_error "Failed to install binary. Installation aborted." "${TEMP_DIR}"

# Clean up and set permissions
rm -rf "${TEMP_DIR}"
chmod 755 /usr/local/bin/x-ui-exporter

# Create config directory
step 5 "Creating configuration directory..."
mkdir -p /etc/x-ui-exporter/
abort_on_error "Failed to create config directory. Installation aborted."

# Check if config file already exists
CONFIG_FILE="/etc/x-ui-exporter/config.yaml"
SKIP_CONFIG_SETUP=0
if [ -f "$CONFIG_FILE" ]; then
    echo "Configuration file already exists at $CONFIG_FILE"
    ask_continue "Do you want to overwrite the existing config?" \
                 "Overwriting existing configuration..." \
                 "Skipping config setup."
    SKIP_CONFIG_SETUP=$(( ! $? ))
fi

if [ $SKIP_CONFIG_SETUP -eq 0 ]; then
    # Download example config file
    echo "Downloading config template from GitHub..."
    curl -s -o "$CONFIG_FILE.tmpl"  https://raw.githubusercontent.com/${GITHUB_REPO}/${SOURCE_BRANCH}/configs/config.yaml.tmpl
    abort_on_error "Failed to download config file. Installation aborted."

    echo "============================================="
    echo " 3X-UI Exporter Configuration"
    echo "============================================="
    echo

    echo "--- Metrics Server ---"
    prompt_input X_UI_EXPORTER_METRICS_LISTEN_IP   "Metrics listen IP"               "0.0.0.0"  validate_ip
    prompt_input X_UI_EXPORTER_METRICS_PORT        "Metrics port"                    "9090"     port
    prompt_input X_UI_EXPORTER_METRICS_PATH        "Metrics path"                    "/metrics" nonempty
    prompt_input X_UI_EXPORTER_METRICS_PROTECTED   "Enable basic auth? (true/false)" "false"    bool

    if [[ "${X_UI_EXPORTER_METRICS_PROTECTED}" == "true" ]]; then
        prompt_input X_UI_EXPORTER_METRICS_USERNAME    "Metrics username"                "metricsUser"                nonempty
        prompt_input X_UI_EXPORTER_METRICS_PASSWORD    "Metrics password"                "MetricsVeryHardPassword"    nonempty --secret
    else
        export X_UI_EXPORTER_METRICS_USERNAME=""
        export X_UI_EXPORTER_METRICS_PASSWORD=""
    fi
    prompt_input X_UI_EXPORTER_UPDATE_INTERVAL     "Polling interval (seconds)"      "30"       number
    prompt_input X_UI_EXPORTER_SCRAPE_TIMEOUT      "Scrape timeout (seconds)"        "10"       number
    prompt_input X_UI_EXPORTER_TIMEZONE            "Timezone"                        "UTC"      nonempty

    echo
    echo "--- 3X-UI Panel Connection ---"
    prompt_input THREEXUI_PANEL_PORT               "Panel port"                   ""             port
    prompt_input THREEXUI_PANEL_PATH               "Panel base path"              ""             nonempty
    prompt_input THREEXUI_PANEL_USERNAME           "Panel username"               ""             nonempty
    prompt_input THREEXUI_PANEL_PASSWORD           "Panel password"               ""             nonempty --secret
    prompt_input THREEXUI_INSECURE_SKIP_VERIFY     "Skip SSL verification? (true/false)" "false" bool
    prompt_input THREEXUI_CLIENTS_BYTES_ROWS       "Clients bytes rows (0 = all)" "0"            number
    prompt_input THREEXUI_PANEL_TIMEOUT            "Request timeout (seconds)"    "10"           number

    # ── Panel connection validation ────────────────────────────────────────
    PANEL_BASE="http://127.0.0.1:${THREEXUI_PANEL_PORT}"
    PANEL_PATH_CLEAN="${THREEXUI_PANEL_PATH#/}"
    PANEL_URL="${PANEL_BASE}/${PANEL_PATH_CLEAN}/login"
    PANEL_URL="${PANEL_URL//\/\/login/\/login}"

    echo "Validating connection to panel..."

    HTTP_CODE=$(curl -s -o /dev/null -w "%{http_code}" -X POST "$PANEL_URL" \
        -H "Content-Type: application/json" \
        -d "{\"username\":\"${THREEXUI_PANEL_USERNAME}\",\"password\":\"${THREEXUI_PANEL_PASSWORD}\"}" \
    ) || { echo "Failed to connect to panel. Network error (curl exit code: $?)."; ask_continue; }

    case "$HTTP_CODE" in
        200|303)
            echo "Successfully connected to panel!"
            ;;
        401|403)
            echo "Authentication failed. Invalid username or password."
            ask_continue || exit 1
            ;;
        *)
            echo "Failed to connect to panel. HTTP status: $HTTP_CODE"
            echo "Please verify your panel URL and credentials."
            ask_continue || exit 1
            ;;
    esac

    # ── Now generate config.yaml from template ─────────────────────────────
    substitute_template $CONFIG_FILE.tmpl $CONFIG_FILE
    rm -rf $CONFIG_FILE.tmpl
    echo "✔ Configuration generated and saved to $CONFIG_FILE."

else
    echo "Using existing configuration file without changes."
fi

chmod 600 "$CONFIG_FILE"
chown -R x-ui-exporter:x-ui-exporter /etc/x-ui-exporter

# Create systemd service file
step 6 "Downloading systemd service file from GitHub..."
curl -s -o /etc/systemd/system/x-ui-exporter.service https://raw.githubusercontent.com/${GITHUB_REPO}/${SOURCE_BRANCH}/init/x-ui-exporter.service
abort_on_error "Failed to create service file. Installation aborted."

sed -i "s|^Description=\(.*\)|Description=\1 ${VERSION}|" /etc/systemd/system/x-ui-exporter.service
chmod 644 /etc/systemd/system/x-ui-exporter.service

# Reload systemd to recognize the new service
step 7 "Reloading systemd daemon..."
systemctl daemon-reload
abort_on_error "Failed to reload systemd. Installation aborted."

# Enable and start (or restart) the service
step 8 "Enabling and starting x-ui-exporter service..."
if systemctl is-active --quiet x-ui-exporter.service; then
    echo "Service is already running. Restarting..."
    systemctl restart x-ui-exporter.service
    abort_on_error "Failed to restart service. Installation aborted."
else
    systemctl enable x-ui-exporter.service
    abort_on_error "Failed to enable service. Installation aborted."

    systemctl start x-ui-exporter.service
    abort_on_error "Failed to start service. Installation aborted."
fi

sudo systemctl status x-ui-exporter --no-pager

echo -e "\n${PURPLE}✅ 3X-UI Exporter is installed!"
echo -e "${GREEN}\nCheck status:      ${NC}sudo systemctl status x-ui-exporter --no-pager"
echo -e "${GREEN}Binary path:       ${NC}/usr/local/bin/x-ui-exporter"
echo -e "${GREEN}Config path:       ${NC}$CONFIG_FILE"
echo ""
echo -e "You can view logs with: journalctl -u x-ui-exporter.service"
echo ""
