#!/bin/bash

# CashStratum Post-Installation Script
# Sets up systemd service and firewall rules

set -e

echo "======================================"
echo "CashStratum Post-Installation Setup"
echo "======================================"
echo

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

# Check if running as root
if [ "$EUID" -ne 0 ]; then
   echo -e "${RED}This script must be run as root (use sudo)${NC}"
   exit 1
fi

# Get the user who called sudo
ACTUAL_USER="${SUDO_USER:-$USER}"
if [ "$ACTUAL_USER" = "root" ]; then
    echo -e "${RED}Please run this script with sudo as a regular user, not as root directly${NC}"
    exit 1
fi

# Determine default directory: /home/$ACTUAL_USER/cashstratum, fallback to /home/$ACTUAL_USER/ckpool if it exists
DEFAULT_INSTALL_DIR="/home/$ACTUAL_USER/cashstratum"
if [ ! -d "$DEFAULT_INSTALL_DIR" ] && [ -d "/home/$ACTUAL_USER/ckpool" ]; then
    DEFAULT_INSTALL_DIR="/home/$ACTUAL_USER/ckpool"
fi

# Prompt for installation directory
echo -e "${YELLOW}Where is CashStratum installed?${NC}"
read -e -r -p "Installation directory (default: $DEFAULT_INSTALL_DIR): " USER_INSTALL_DIR
INSTALL_DIR="${USER_INSTALL_DIR:-$DEFAULT_INSTALL_DIR}"

# Verify installation directory exists
if [ ! -d "$INSTALL_DIR" ]; then
    echo -e "${RED}Error: Directory $INSTALL_DIR does not exist${NC}"
    exit 1
fi

# Verify daemon binary exists (look for cashstratum, fallback to ckpool)
if [ ! -f "$INSTALL_DIR/cashstratum" ] && [ ! -f "$INSTALL_DIR/ckpool" ]; then
    echo -e "${RED}Error: cashstratum/ckpool binary not found in $INSTALL_DIR${NC}"
    echo "Please run install-cashstratum.sh first"
    exit 1
fi

# Ensure binary symlink
if [ -f "$INSTALL_DIR/cashstratum" ] && [ ! -f "$INSTALL_DIR/ckpool" ]; then
    ln -sf cashstratum "$INSTALL_DIR/ckpool"
elif [ -f "$INSTALL_DIR/ckpool" ] && [ ! -f "$INSTALL_DIR/cashstratum" ]; then
    ln -sf ckpool "$INSTALL_DIR/cashstratum"
fi

echo
echo "Installation directory: $INSTALL_DIR"
echo "Running as user: $ACTUAL_USER"
echo

# Function to extract ports from JSON config
extract_ports_from_config() {
    local config_file="$1"
    local ports=()

    if [ -f "$config_file" ]; then
        # Extract serverurl ports using grep and sed
        local server_ports
        server_ports=$(grep -o '"serverurl"[[:space:]]*:[[:space:]]*\[[^]]*\]' "$config_file" | \
            grep -o '"[^"]*:[0-9]*"' | \
            sed 's/.*:\([0-9]*\)"/\1/')

        for port in $server_ports; do
            ports+=("$port")
        done
    fi

    echo "${ports[@]}"
}

# Detect ports from configs: look for cashstratum.conf, then ckpool.conf
echo "Detecting ports from configuration files..."
PORTS=()

CONF_FILE=""
if [ -f "$INSTALL_DIR/cashstratum.conf" ]; then
    CONF_FILE="$INSTALL_DIR/cashstratum.conf"
elif [ -f "$INSTALL_DIR/ckpool.conf" ]; then
    CONF_FILE="$INSTALL_DIR/ckpool.conf"
fi

TESTNET_CONF_FILE=""
if [ -f "$INSTALL_DIR/cashstratum-testnet.conf" ]; then
    TESTNET_CONF_FILE="$INSTALL_DIR/cashstratum-testnet.conf"
elif [ -f "$INSTALL_DIR/ckpool-testnet.conf" ]; then
    TESTNET_CONF_FILE="$INSTALL_DIR/ckpool-testnet.conf"
fi

# Check main config
if [ -n "$CONF_FILE" ]; then
    echo "Reading $CONF_FILE..."
    # shellcheck disable=SC2207
    MAIN_PORTS=($(extract_ports_from_config "$CONF_FILE"))
    PORTS+=("${MAIN_PORTS[@]}")
fi

# Check testnet config
if [ -n "$TESTNET_CONF_FILE" ]; then
    echo "Reading $TESTNET_CONF_FILE..."
    # shellcheck disable=SC2207
    TESTNET_PORTS=($(extract_ports_from_config "$TESTNET_CONF_FILE"))
    PORTS+=("${TESTNET_PORTS[@]}")
fi

# Remove duplicates
# shellcheck disable=SC2207
PORTS=($(echo "${PORTS[@]}" | tr ' ' '\n' | sort -u | tr '\n' ' '))

echo -e "${GREEN}Detected ports: ${PORTS[*]}${NC}"
echo

# Ask if user wants to configure firewall
if command -v ufw &> /dev/null; then
    echo -e "${YELLOW}Configure firewall (UFW)?${NC}"
    read -p "This will open ports: ${PORTS[*]} (y/n) " -n 1 -r
    echo

    if [[ $REPLY =~ ^[Yy]$ ]]; then
        echo "Configuring UFW firewall rules..."

        # Ensure UFW is enabled
        if ! ufw status | grep -q "Status: active"; then
            echo "Enabling UFW..."
            ufw --force enable
        fi

        # Open detected ports
        for port in "${PORTS[@]}"; do
            echo "Opening port $port/tcp for stratum mining..."
            ufw allow "$port/tcp" comment "CashStratum stratum port"
        done

        # Ask about SSH if not already allowed
        if ! ufw status | grep -q "22/tcp"; then
            echo -e "${YELLOW}Warning: SSH port 22 is not open. Add it now? (y/n)${NC}"
            read -p "" -n 1 -r
            echo
            if [[ $REPLY =~ ^[Yy]$ ]]; then
                ufw allow 22/tcp comment "SSH"
            fi
        fi

        echo -e "${GREEN}✓ Firewall rules configured${NC}"
        ufw status numbered
    fi
else
    echo -e "${YELLOW}UFW not found. Skipping firewall configuration.${NC}"
fi

# ---------------------------------------------------------------------------
# Go log API port -- deliberately NOT opened to the world.
#
# The API key is the only authentication and it travels in cleartext over
# HTTP, so a blanket `ufw allow 8888/tcp` hands the whole pool's log tree to
# anyone who guesses or sniffs the token. The rule is scoped to the single
# address that consumes the API, exactly as api/README.md requires.
# Leaving the answer blank keeps the port loopback-only, which is correct when
# the consuming app runs on this same host.
# ---------------------------------------------------------------------------
API_ENV=""
if [ -f "$INSTALL_DIR/api/cashstratum-api.env" ]; then
    API_ENV="$INSTALL_DIR/api/cashstratum-api.env"
elif [ -f "$INSTALL_DIR/api/ckpool-api.env" ]; then
    API_ENV="$INSTALL_DIR/api/ckpool-api.env"
fi

if command -v ufw &> /dev/null && [ -n "$API_ENV" ]; then
    APIPORT=$(sed -n -e 's/^CASHSTRATUM_API_PORT=//p' -e 's/^CKPOOL_API_PORT=//p' "$API_ENV" | tail -1)
    APIPORT="${APIPORT:-8888}"
    echo
    echo -e "${YELLOW}Go log API detected (port $APIPORT).${NC}"
    echo "This port must NOT face the internet - the key is cleartext over HTTP."
    echo "Enter the address allowed to reach it (e.g. 203.0.113.10, or"
    echo "10.0.0.0/24). Leave BLANK for loopback-only, which is right if the"
    echo "app that consumes it runs on this machine."
    read -r -p "Allowed source [blank = localhost only]: " API_SRC

    if [ -z "$API_SRC" ]; then
        echo -e "${GREEN}✓ No firewall rule added - API reachable only on 127.0.0.1:$APIPORT${NC}"
    elif [[ "$API_SRC" =~ ^(0\.0\.0\.0(/0)?|any|ANY|\*)$ ]]; then
        echo -e "${RED}✗ Refusing to expose the log API to the whole internet.${NC}"
        echo "  Re-run and give a specific host or CIDR, or leave it blank."
    else
        ufw allow from "$API_SRC" to any port "$APIPORT" proto tcp \
            comment "CashStratum log API" \
            && echo -e "${GREEN}✓ Port $APIPORT opened for $API_SRC only${NC}" \
            || echo -e "${RED}✗ ufw rejected that address - check the format${NC}"
    fi
fi

echo

# Create systemd service file
echo "Creating systemd service..."

DAEMON_BIN="$INSTALL_DIR/cashstratum"
[ -x "$DAEMON_BIN" ] || DAEMON_BIN="$INSTALL_DIR/ckpool"
MAIN_CONF="${CONF_FILE:-$INSTALL_DIR/cashstratum.conf}"

SERVICE_FILE="/etc/systemd/system/cashstratum.service"
cat > "$SERVICE_FILE" << EOF
[Unit]
Description=CashStratum Bitcoin Cash Mining Pool (Solo Mode with Auto-Pay)
Documentation=https://github.com/cashstratum/cashstratum
After=network.target network-online.target
Wants=network-online.target

[Service]
Type=simple
User=$ACTUAL_USER
Group=$ACTUAL_USER
WorkingDirectory=$INSTALL_DIR
ExecStart=$DAEMON_BIN -c $MAIN_CONF -n cashstratum -L -B
ExecReload=/bin/kill -HUP \$MAINPID
Restart=on-failure
RestartSec=10
TimeoutStartSec=60
TimeoutStopSec=30

# Resource limits for high-performance mining
LimitNOFILE=2100000
LimitNPROC=32768
LimitMEMLOCK=infinity

# Security settings
NoNewPrivileges=true
PrivateTmp=true

# Environment
Environment="PATH=/usr/local/bin:/usr/bin:/bin"

# Process management
KillMode=mixed
KillSignal=SIGTERM

[Install]
WantedBy=multi-user.target
Alias=ckpool.service
EOF

ln -sf cashstratum.service /etc/systemd/system/ckpool.service
echo -e "${GREEN}✓ Systemd service created (cashstratum.service and ckpool.service symlink)${NC}"

# Create systemd service file for testnet (optional)
if [ -n "$TESTNET_CONF_FILE" ]; then
    echo "Creating testnet systemd service..."

    TESTNET_SERVICE_FILE="/etc/systemd/system/cashstratum-testnet.service"
    cat > "$TESTNET_SERVICE_FILE" << EOF
[Unit]
Description=CashStratum Bitcoin Cash Mining Pool (Testnet - Solo Mode with Auto-Pay)
Documentation=https://github.com/cashstratum/cashstratum
After=network.target network-online.target
Wants=network-online.target

[Service]
Type=simple
User=$ACTUAL_USER
Group=$ACTUAL_USER
WorkingDirectory=$INSTALL_DIR
ExecStart=$DAEMON_BIN -c $TESTNET_CONF_FILE -n cashstratum-testnet -L -B
ExecReload=/bin/kill -HUP \$MAINPID
Restart=on-failure
RestartSec=10
TimeoutStartSec=60
TimeoutStopSec=30

# Resource limits for high-performance mining
LimitNOFILE=2100000
LimitNPROC=32768
LimitMEMLOCK=infinity

# Security settings
NoNewPrivileges=true
PrivateTmp=true

# Environment
Environment="PATH=/usr/local/bin:/usr/bin:/bin"

# Process management
KillMode=mixed
KillSignal=SIGTERM

[Install]
WantedBy=multi-user.target
Alias=ckpool-testnet.service
EOF

    ln -sf cashstratum-testnet.service /etc/systemd/system/ckpool-testnet.service
    echo -e "${GREEN}✓ Testnet systemd service created${NC}"
fi

# ---------------------------------------------------------------------------
# Go log API service (only if built)
#
# Runs as the actual user on purpose: daemon creates its logdir mode 0750, so
# any other account reads "permission denied" from every endpoint while
# /health still answers, which is a confusing way to find that out.
# ---------------------------------------------------------------------------
API_DIR="$INSTALL_DIR/api"
API_BIN=""
if [ -x "$API_DIR/cashstratum-api" ]; then
    API_BIN="$API_DIR/cashstratum-api"
elif [ -x "$API_DIR/ckpool-api" ]; then
    API_BIN="$API_DIR/ckpool-api"
fi

API_ENV=""
if [ -f "$API_DIR/cashstratum-api.env" ]; then
    API_ENV="$API_DIR/cashstratum-api.env"
elif [ -f "$API_DIR/ckpool-api.env" ]; then
    API_ENV="$API_DIR/ckpool-api.env"
fi

API_SERVICE_FILE="/etc/systemd/system/cashstratum-api.service"
API_INSTALLED=false
API_PORT=8888

if [ -n "$API_BIN" ] && [ -n "$API_ENV" ]; then
    API_INSTALLED=true
    # Ensure symlinks between cashstratum-api and ckpool-api
    [ -f "$API_DIR/cashstratum-api" ] && [ ! -f "$API_DIR/ckpool-api" ] && ln -sf cashstratum-api "$API_DIR/ckpool-api"
    [ -f "$API_DIR/cashstratum-api.env" ] && [ ! -f "$API_DIR/ckpool-api.env" ] && ln -sf cashstratum-api.env "$API_DIR/ckpool-api.env"
    [ -f "$API_DIR/ckpool-api.env" ] && [ ! -f "$API_DIR/cashstratum-api.env" ] && ln -sf ckpool-api.env "$API_DIR/cashstratum-api.env"

    ENVPORT=$(sed -n -e 's/^CASHSTRATUM_API_PORT=//p' -e 's/^CKPOOL_API_PORT=//p' "$API_ENV" | tail -1)
    [ -n "$ENVPORT" ] && API_PORT="$ENVPORT"

    echo
    echo "Creating cashstratum-api systemd service..."

    # The env file holds the API key. Root-owned, 0600: systemd reads it as
    # root before dropping to User=, so the service account never needs it.
    chown root:root "$API_DIR"/*api.env 2>/dev/null || true
    chmod 600 "$API_DIR"/*api.env 2>/dev/null || true

    cat > "$API_SERVICE_FILE" <<APISVC
[Unit]
Description=CashStratum Log API (Go)
Documentation=https://github.com/cashstratum/cashstratum/blob/main/api/README.md
After=network-online.target cashstratum.service
Wants=network-online.target
StartLimitIntervalSec=60
StartLimitBurst=3

[Service]
Type=simple
User=$ACTUAL_USER
Group=$ACTUAL_USER
WorkingDirectory=$API_DIR
ExecStart=$API_BIN

# Never use Environment= for the key: unit files are world readable and
# \`systemctl show\` prints them to any user.
EnvironmentFile=$API_ENV

Restart=always
RestartSec=5

# The API must never be able to disturb the pool it reports on.
MemoryMax=128M
MemoryAccounting=true
CPUQuota=25%
TasksMax=50
LimitNOFILE=4096

NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=read-only
ReadOnlyPaths=$INSTALL_DIR
ProtectKernelTunables=true
ProtectControlGroups=true
RestrictSUIDSGID=true

StandardOutput=journal
StandardError=journal
SyslogIdentifier=cashstratum-api

KillMode=mixed
KillSignal=SIGTERM
TimeoutStopSec=10

[Install]
WantedBy=multi-user.target
Alias=ckpool-api.service
APISVC

    ln -sf cashstratum-api.service /etc/systemd/system/ckpool-api.service
    echo -e "${GREEN}✓ cashstratum-api service created (port $API_PORT)${NC}"
fi

# Reload systemd
echo "Reloading systemd daemon..."
systemctl daemon-reload

# Enable services
echo
echo -e "${YELLOW}Enable CashStratum to start on boot?${NC}"
read -p "(y/n) " -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
    systemctl enable cashstratum.service
    echo -e "${GREEN}✓ CashStratum service enabled${NC}"

    if [ -n "$TESTNET_SERVICE_FILE" ]; then
        echo -e "${YELLOW}Enable CashStratum Testnet to start on boot?${NC}"
        read -p "(y/n) " -n 1 -r
        echo
        if [[ $REPLY =~ ^[Yy]$ ]]; then
            systemctl enable cashstratum-testnet.service
            echo -e "${GREEN}✓ CashStratum Testnet service enabled${NC}"
        fi
    fi
fi

if [ "$API_INSTALLED" = true ]; then
    echo
    echo -e "${YELLOW}Enable the Go log API to start on boot?${NC}"
    read -p "(y/n) " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        systemctl enable cashstratum-api.service
        systemctl restart cashstratum-api.service 2>/dev/null || true
        sleep 1
        if systemctl is-active --quiet cashstratum-api.service 2>/dev/null; then
            echo -e "${GREEN}✓ cashstratum-api enabled and running on port $API_PORT${NC}"
        else
            echo -e "${RED}✗ cashstratum-api failed to start. Check: journalctl -u cashstratum-api -n 40${NC}"
        fi
    fi
fi

# Create monitor script
echo
echo "Creating monitoring and maintenance scripts..."

MONITOR_SCRIPT="$INSTALL_DIR/monitor.sh"
cat > "$MONITOR_SCRIPT" << 'MONITOR_EOF'
#!/bin/bash

# CashStratum Monitor - Matrix Style
# Colors
GREEN='\033[0;32m'
BRIGHT_GREEN='\033[1;32m'
CYAN='\033[0;36m'
BRIGHT_CYAN='\033[1;36m'
YELLOW='\033[0;33m'
BRIGHT_YELLOW='\033[1;33m'
RED='\033[0;31m'
BRIGHT_RED='\033[1;31m'
MAGENTA='\033[0;35m'
BRIGHT_MAGENTA='\033[1;35m'
BLUE='\033[0;34m'
BRIGHT_BLUE='\033[1;34m'
WHITE='\033[1;37m'
GRAY='\033[0;37m'
DIM='\033[2m'
NC='\033[0m'

# Configuration - use environment variable or default
CASHSTRATUM_DIR="${CASHSTRATUM_DIR:-${CKPOOL_DIR:-$HOME/cashstratum}}"
[ -d "$CASHSTRATUM_DIR" ] || [ ! -d "$HOME/ckpool" ] || CASHSTRATUM_DIR="$HOME/ckpool"
LOG_FILE="$CASHSTRATUM_DIR/logs/cashstratum.log"
[ -f "$LOG_FILE" ] || LOG_FILE="$CASHSTRATUM_DIR/logs/ckpool.log"

# Check if log file exists
if [ ! -f "$LOG_FILE" ]; then
    echo -e "${RED}Error: Log file not found at $LOG_FILE${NC}"
    echo "Set CASHSTRATUM_DIR environment variable or ensure CashStratum is installed at $CASHSTRATUM_DIR"
    exit 1
fi

# Clear screen
clear

# Simple header
echo -e "${BRIGHT_GREEN}▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓${NC}"
echo -e "${BRIGHT_GREEN}                    CASHSTRATUM MONITOR SYSTEM                         ${NC}"
echo -e "${BRIGHT_GREEN}▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓${NC}"
echo -e "${DIM}Monitoring: $LOG_FILE${NC}"
echo -e "${DIM}Press Ctrl+C to exit${NC}"
echo

# Monitor logs
while IFS= read -r line; do
    # Different colors for different log types
    if echo "$line" | grep -q "Block hash changed to"; then
        echo -e "${BRIGHT_MAGENTA}${line}${NC}"
    elif echo "$line" | grep -q "BLOCK"; then
        echo -e "${BRIGHT_GREEN}${line}${NC}"
    elif echo "$line" | grep -q "Authorised client"; then
        echo -e "${GREEN}${line}${NC}"
    elif echo "$line" | grep -q "Network diff set to"; then
        echo -e "${BRIGHT_YELLOW}${line}${NC}"
    elif echo "$line" | grep -q 'User.*hashrate.*"hashrate1m"'; then
        echo -e "${BRIGHT_CYAN}${line}${NC}"
    elif echo "$line" | grep -q 'Pool:{"hashrate1m"'; then
        echo -e "${YELLOW}${line}${NC}"
    elif echo "$line" | grep -q 'Pool:{"diff"'; then
        echo -e "${CYAN}${line}${NC}"
    elif echo "$line" | grep -q 'Pool:{"runtime"'; then
        echo -e "${MAGENTA}${line}${NC}"
    elif echo "$line" | grep -q "ZMQ"; then
        echo -e "${BRIGHT_BLUE}${line}${NC}"
    elif echo "$line" | grep -q "Stored local workbase"; then
        echo -e "${GRAY}${line}${NC}"
    elif echo "$line" | grep -q "Failed over to bitcoind"; then
        echo -e "${BRIGHT_YELLOW}${line}${NC}"
    elif echo "$line" | grep -q "Server alive"; then
        echo -e "${BRIGHT_GREEN}${line}${NC}"
    elif echo "$line" | grep -q "ERROR\|error"; then
        echo -e "${BRIGHT_RED}${line}${NC}"
    elif echo "$line" | grep -q "Disconnected"; then
        echo -e "${RED}${line}${NC}"
    elif echo "$line" | grep -q "Connected"; then
        echo -e "${GREEN}${line}${NC}"
    elif echo "$line" | grep -q "shares"; then
        echo -e "${CYAN}${line}${NC}"
    elif echo "$line" | grep -q "accepted\|rejected"; then
        echo -e "${WHITE}${line}${NC}"
    else
        echo -e "${GREEN}${line}${NC}"
    fi
done < <(tail -f "$LOG_FILE" 2>/dev/null)
MONITOR_EOF

chmod +x "$MONITOR_SCRIPT"
chown "$ACTUAL_USER:$ACTUAL_USER" "$MONITOR_SCRIPT"

# Also create in user's home directory for easy access
cp "$MONITOR_SCRIPT" "/home/$ACTUAL_USER/monitor.sh"
chmod +x "/home/$ACTUAL_USER/monitor.sh"
chown "$ACTUAL_USER:$ACTUAL_USER" "/home/$ACTUAL_USER/monitor.sh"

echo -e "${GREEN}✓ Monitor script created${NC}"

# Setup cleanup script with solve-index protection
CLEANUP_SCRIPT="$INSTALL_DIR/clean-old-blocks.sh"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Ensure clean-old-blocks.sh exists in $INSTALL_DIR (copy the real script from source repo if needed)
if [ ! -f "$CLEANUP_SCRIPT" ] && [ -f "$SCRIPT_DIR/clean-old-blocks.sh" ]; then
    cp "$SCRIPT_DIR/clean-old-blocks.sh" "$CLEANUP_SCRIPT"
fi

if [ -f "$CLEANUP_SCRIPT" ]; then
    chmod +x "$CLEANUP_SCRIPT"
    chown "$ACTUAL_USER:$ACTUAL_USER" "$CLEANUP_SCRIPT"
    echo -e "${GREEN}✓ clean-old-blocks.sh configured with solve-index protection${NC}"
else
    echo -e "${RED}Warning: clean-old-blocks.sh not found at $CLEANUP_SCRIPT or $SCRIPT_DIR${NC}"
fi

# Add cron job for cleanup
echo
echo -e "${YELLOW}Add daily cleanup task to crontab?${NC}"
echo "This will run clean-old-blocks.sh at 3 AM daily (with solve-index protection)"
read -p "(y/n) " -n 1 -r
echo

if [[ $REPLY =~ ^[Yy]$ ]] && [ -f "$CLEANUP_SCRIPT" ]; then
    # Check if cron job already exists
    CRON_JOB="0 3 * * * $CLEANUP_SCRIPT"
    (crontab -u "$ACTUAL_USER" -l 2>/dev/null | grep -v "clean-old-blocks.sh"; echo "$CRON_JOB") | crontab -u "$ACTUAL_USER" -

    echo -e "${GREEN}✓ Cron job added (runs clean-old-blocks.sh daily at 3 AM)${NC}"
fi

echo
echo "======================================"
echo -e "${GREEN}Post-Installation Complete!${NC}"
echo "======================================"
echo
echo "Service files created:"
echo "  • /etc/systemd/system/cashstratum.service (alias: ckpool.service)"
[ -n "$TESTNET_SERVICE_FILE" ] && echo "  • /etc/systemd/system/cashstratum-testnet.service (alias: ckpool-testnet.service)"
[ "$API_INSTALLED" = true ] && echo "  • /etc/systemd/system/cashstratum-api.service (alias: ckpool-api.service)"
echo
echo "Scripts configured:"
echo "  • $INSTALL_DIR/monitor.sh"
echo "  • /home/$ACTUAL_USER/monitor.sh (shortcut)"
echo "  • $INSTALL_DIR/clean-old-blocks.sh (with solve-index protection)"
echo
echo "Firewall ports opened:"
for port in "${PORTS[@]}"; do
    echo "  • $port/tcp (stratum mining)"
done
echo
echo -e "${YELLOW}Before starting, please configure:${NC}"
echo "1. Edit ${CONF_FILE:-$INSTALL_DIR/cashstratum.conf} with your BCH node credentials"
echo "2. Update bchaddress with your mining address (fallback for non-address usernames)"
echo "3. Update pooladdress with your pool operator fee address"
echo "4. Set poolfee to desired percentage (default 2.0%, configurable 0-50)"
echo
echo "Service management commands:"
echo "  sudo systemctl start cashstratum        # Start the pool"
echo "  sudo systemctl stop cashstratum         # Stop the pool"
echo "  sudo systemctl restart cashstratum      # Restart the pool"
echo "  sudo systemctl status cashstratum       # Check status"
echo "  sudo journalctl -u cashstratum -f       # View logs"
echo
if [ -n "$TESTNET_SERVICE_FILE" ]; then
    echo "Testnet service commands:"
    echo "  sudo systemctl start cashstratum-testnet"
    echo "  sudo systemctl status cashstratum-testnet"
    echo "  sudo journalctl -u cashstratum-testnet -f"
    echo
fi
echo "Monitoring commands:"
echo "  ~/monitor.sh                            # Run colorized log monitor"
echo "  sudo journalctl -u cashstratum -f       # View systemd logs"
echo
echo "Maintenance:"
echo "  $CLEANUP_SCRIPT                         # Manual cleanup (runs daily at 3 AM via cron)"
echo
echo -e "${GREEN}Ready to start mining!${NC}"
echo
