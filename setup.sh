#!/usr/bin/env bash
# shellcheck disable=SC2005  # L prints without a newline; the echo around it adds one.
#
# Tolikopa — server installation and update (Ubuntu).
#
#   sudo ./setup.sh             first run: asks the questions below, then builds and starts
#   sudo ./setup.sh             later runs (deploy/.env exists): rebuild + restart, no questions
#   sudo ./setup.sh --lang en   skip the language question (tr | en)
#
# On a terminal it first asks for the language (Turkish / English); without a
# terminal it uses TOLIKOPA_LANG or --lang, else Turkish.
#
# First run asks for:
#   - the panel's DNS name (e.g. tolikopa.corp.local) and this server's IP address;
#     a self-signed certificate is created for exactly these two names,
#   - the first administrator's username and password.
#
# Nothing is installed by this script. It checks what the server needs first and,
# if something is missing, stops and prints the commands that install it.
#
# For unattended runs the answers can come from the environment instead:
#   TOLIKOPA_HOST, TOLIKOPA_IP, TOLIKOPA_ADMIN_USER, TOLIKOPA_LANG and the password as
#   TOLIKOPA_ADMIN_PASSWORD_FILE=/root/tolikopa-admin.pw   (preferred: a file owned by
#     root with mode 600 or 400; its first line is the password)
#   TOLIKOPA_ADMIN_PASSWORD=...   (works, but `sudo VAR=... ./setup.sh` shows it in ps
#     and in the shell history)

set -Eeuo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEPLOY_DIR="$REPO_ROOT/deploy"
ENV_FILE="$DEPLOY_DIR/.env"
TLS_DIR="$DEPLOY_DIR/tls"
CERT="$TLS_DIR/tolikopa.crt"
KEY="$TLS_DIR/tolikopa.key"
CERT_DAYS=3650

MIN_COMPOSE=2.20.0
MIN_BUILDX=0.17.0
MIN_ENGINE=24.0.0
MIN_FREE_GB=10

UI_LANG="${TOLIKOPA_LANG:-}"
while [ $# -gt 0 ]; do
  case "$1" in
    --lang) UI_LANG="${2:-}"; shift 2 ;;
    --lang=*) UI_LANG="${1#--lang=}"; shift ;;
    -h|--help) sed -n '4,26p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "unknown option: $1" >&2; exit 2 ;;
  esac
done
# No --lang / TOLIKOPA_LANG: ask on a terminal, the system locale suggests the default.
if [ -z "$UI_LANG" ] && [ -t 0 ] && [ -t 1 ]; then
  lang_def=en
  case "${LC_ALL:-${LANG:-}}" in tr*) lang_def="tr" ;; esac
  echo
  echo "  1) Türkçe"
  echo "  2) English"
  read -r -p "  Dil / Language [$([ "$lang_def" = tr ] && echo 1 || echo 2)]: " lang_ans || true
  case "${lang_ans:-}" in
    1|[tT]*) UI_LANG="tr" ;;
    2|[eE]*) UI_LANG=en ;;
    *) UI_LANG="$lang_def" ;;
  esac
fi
case "$UI_LANG" in en|EN|En) UI_LANG=en ;; *) UI_LANG="tr" ;; esac

# L "Türkçe" "English" — prints the text in the chosen language.
L() { if [ "$UI_LANG" = en ]; then printf '%s' "$2"; else printf '%s' "$1"; fi; }

if [ -t 1 ]; then
  c_g=$'\033[0;32m'; c_y=$'\033[0;33m'; c_r=$'\033[0;31m'; c_b=$'\033[0;36m'; c_d=$'\033[2m'; c_0=$'\033[0m'
else
  c_g=; c_y=; c_r=; c_b=; c_d=; c_0=
fi
say()  { printf '%s»%s %s\n' "$c_b" "$c_0" "$*"; }
ok()   { printf '%s✓%s %s\n' "$c_g" "$c_0" "$*"; }
warn() { printf '%s!%s %s\n' "$c_y" "$c_0" "$*"; }
die()  { printf '%s✗ %s%s\n' "$c_r" "$*" "$c_0" >&2; exit 1; }
trap 'die "$(L "Beklenmeyen hata (satır $LINENO). Yukarıdaki çıktıya bakın." "Unexpected error (line $LINENO). See the output above.")"' ERR

dc() { docker compose --project-directory "$DEPLOY_DIR" -f "$DEPLOY_DIR/docker-compose.yml" "$@"; }

# ver_ge A B — true when version A >= B.
ver_ge() { [ "$(printf '%s\n%s\n' "$2" "$1" | sort -V | head -n1)" = "$2" ]; }

# ── 0) Where and who ─────────────────────────────────────────────────────────
[ "$(id -u)" -eq 0 ] || die "$(L "Root olarak çalıştırın:  sudo ./setup.sh" "Run as root:  sudo ./setup.sh")"
{ [ -f "$DEPLOY_DIR/docker-compose.yml" ] && [ -f "$REPO_ROOT/server/Dockerfile" ]; } \
  || die "$(L "setup.sh deponun kök klasöründe olmalı (deploy/ ve server/ ile birlikte)." "setup.sh must sit in the repository root (next to deploy/ and server/).")"

echo
printf '%sTolikopa%s — %s\n' "$c_b" "$c_0" "$(L "sunucu kurulumu" "server setup")"
echo "────────────────────────────────────────────────────────────"

# ── 1) Prerequisites (checked, never installed) ──────────────────────────────
say "$(L "Gereksinimler denetleniyor..." "Checking prerequisites...")"
missing=()      # human-readable problems
fix=()          # commands that fix them
need_docker_install=0
docker_down=0
add_fix() { local c; for c in "$@"; do [[ " ${fix[*]-} " == *" $c "* ]] || fix+=("$c"); done; }

if [ -r /etc/os-release ]; then . /etc/os-release; else ID=unknown; fi
if [ "${ID:-}" != ubuntu ]; then
  warn "$(L "Bu betik Ubuntu için yazıldı (bulunan: ${PRETTY_NAME:-bilinmiyor}). Komut önerileri Ubuntu içindir." "This script targets Ubuntu (found: ${PRETTY_NAME:-unknown}). The suggested commands are for Ubuntu.")"
fi

apt_pkgs=()
for pair in curl:curl openssl:openssl ip:iproute2 ss:iproute2 systemctl:systemd awk:gawk sort:coreutils; do
  cmd="${pair%%:*}"; pkg="${pair#*:}"
  if ! command -v "$cmd" >/dev/null 2>&1; then
    missing+=("$(L "'$cmd' komutu yok" "command '$cmd' not found")")
    [[ " ${apt_pkgs[*]-} " == *" $pkg "* ]] || apt_pkgs+=("$pkg")
  fi
done
if [ ${#apt_pkgs[@]} -gt 0 ]; then
  add_fix "sudo apt-get update" "sudo apt-get install -y ${apt_pkgs[*]}"
fi

if ! command -v docker >/dev/null 2>&1; then
  missing+=("$(L "Docker Engine kurulu değil" "Docker Engine is not installed")")
  need_docker_install=1
else
  if ! docker info >/dev/null 2>&1; then
    missing+=("$(L "Docker çalışmıyor" "Docker is not running")")
    docker_down=1
  else
    engine="$(docker version --format '{{.Server.Version}}' 2>/dev/null || echo 0)"
    if ! ver_ge "${engine#v}" "$MIN_ENGINE"; then
      missing+=("$(L "Docker Engine $engine eski (en az $MIN_ENGINE)" "Docker Engine $engine is too old (need $MIN_ENGINE+)")")
      need_docker_install=1
    fi
  fi
  if ! compose_v="$(docker compose version --short 2>/dev/null)"; then
    missing+=("$(L "Docker Compose eklentisi yok" "the Docker Compose plugin is missing")")
    need_docker_install=1
  elif ! ver_ge "${compose_v#v}" "$MIN_COMPOSE"; then
    missing+=("$(L "Docker Compose $compose_v eski (en az $MIN_COMPOSE)" "Docker Compose $compose_v is too old (need $MIN_COMPOSE+)")")
    need_docker_install=1
  fi
  buildx_v="$(docker buildx version 2>/dev/null | awk '{print $2}' || true)"
  if [ -z "$buildx_v" ]; then
    missing+=("$(L "Docker Buildx eklentisi yok" "the Docker Buildx plugin is missing")")
    need_docker_install=1
  elif ! ver_ge "${buildx_v#v}" "$MIN_BUILDX"; then
    missing+=("$(L "Docker Buildx $buildx_v eski (en az $MIN_BUILDX)" "Docker Buildx $buildx_v is too old (need $MIN_BUILDX+)")")
    need_docker_install=1
  fi
fi
if [ "$need_docker_install" -eq 1 ]; then
  # Docker's official repository (docs.docker.com/engine/install/ubuntu). Added
  # as one block, without de-duplication: the second apt-get update is required.
  fix+=("sudo apt-get update" \
    "sudo apt-get install -y ca-certificates curl" \
    "sudo install -m 0755 -d /etc/apt/keyrings" \
    "sudo curl -fsSL https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc" \
    "sudo chmod a+r /etc/apt/keyrings/docker.asc" \
    "echo \"deb [arch=\$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/ubuntu \$(. /etc/os-release && echo \"\$VERSION_CODENAME\") stable\" | sudo tee /etc/apt/sources.list.d/docker.list > /dev/null" \
    "sudo apt-get update" \
    "sudo apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin" \
    "sudo systemctl enable --now docker")
elif [ "$docker_down" -eq 1 ]; then
  add_fix "sudo systemctl enable --now docker"
fi

# Ports 80/443 — unless it is this stack that holds them (an update run).
stack_running=0
if docker info >/dev/null 2>&1 && [ -n "$(dc ps --status running -q web 2>/dev/null || true)" ]; then
  stack_running=1
fi
if [ "$stack_running" -eq 0 ] && command -v ss >/dev/null 2>&1; then
  for port in 80 443; do
    holder="$(ss -Hltnp "sport = :$port" 2>/dev/null | sed -n 's/.*users:(("\([^"]*\)".*/\1/p' | sort -u | tr '\n' ' ')"
    # A container's published port often shows no process (no userland proxy).
    if [ -z "$holder" ] && docker info >/dev/null 2>&1; then
      ctr="$(docker ps --filter "publish=$port" --format '{{.Names}}' 2>/dev/null | head -n1 || true)"
      [ -z "$ctr" ] || holder="docker container $ctr"
    fi
    if [ -n "$(ss -Hltn "sport = :$port" 2>/dev/null)" ]; then
      missing+=("$(L "$port portu kullanımda (${holder:-bilinmeyen süreç})" "port $port is in use (${holder:-unknown process})")")
      case "$holder" in
        "docker container "*) add_fix "sudo docker stop ${holder#docker container }    # $(L "ya da o projeyi başka bir porta taşıyın" "or move that project to another port")" ;;
        *apache2*) add_fix "sudo systemctl disable --now apache2" ;;
        *nginx*)   add_fix "sudo systemctl disable --now nginx" ;;
        *caddy*)   add_fix "sudo systemctl disable --now caddy" ;;
        *docker*)  add_fix "sudo docker ps    # $(L "portu tutan container'ı durdurun" "stop the container that holds the port")" ;;
        *)         add_fix "sudo ss -ltnp 'sport = :$port'    # $(L "portu kullanan servisi durdurun" "stop the service using the port")" ;;
      esac
    fi
  done
fi

# Disk space where Docker keeps images (the build needs several GB).
if docker info >/dev/null 2>&1; then
  droot="$(docker info --format '{{.DockerRootDir}}' 2>/dev/null || echo /var/lib/docker)"
  [ -d "$droot" ] || droot=/
  free_gb=$(( $(df -Pk "$droot" | awk 'NR==2{print $4}') / 1024 / 1024 ))
  if [ "$free_gb" -lt "$MIN_FREE_GB" ]; then
    missing+=("$(L "$droot için boş alan ${free_gb} GB (en az $MIN_FREE_GB GB gerekli)" "only ${free_gb} GB free for $droot (need $MIN_FREE_GB GB)")")
    add_fix "sudo docker system prune -a    # $(L "kullanılmayan imajları siler" "removes unused images")"
  fi
fi

# The build downloads base images, Go modules and npm packages.
if command -v curl >/dev/null 2>&1; then
  for u in https://registry-1.docker.io/v2/ https://proxy.golang.org/ https://registry.npmjs.org/; do
    if ! curl -sS -o /dev/null --max-time 15 "$u" 2>/dev/null; then
      missing+=("$(L "$u adresine erişilemiyor (internet/proxy)" "cannot reach $u (internet/proxy)")")
      add_fix "# $(L "Sunucunun internete çıkışını açın; proxy varsa Docker için de tanımlayın:" "Allow outbound internet; with a proxy, configure Docker too:") https://docs.docker.com/engine/daemon/proxy/"
    fi
  done
fi

mem_mb=$(( $(awk '/^MemTotal:/{print $2}' /proc/meminfo 2>/dev/null || echo 0) / 1024 ))
if [ "$mem_mb" -gt 0 ] && [ "$mem_mb" -lt 1900 ]; then
  warn "$(L "Bellek ${mem_mb} MB; derleme için en az 2 GB önerilir." "${mem_mb} MB of memory; 2 GB or more is recommended for the build.")"
fi

if [ ${#missing[@]} -gt 0 ]; then
  echo
  printf '%s%s%s\n' "$c_r" "$(L "Kurulum başlatılamadı — eksikler:" "Cannot continue — missing:")" "$c_0"
  for m in "${missing[@]}"; do echo "  - $m"; done
  if [ ${#fix[@]} -gt 0 ]; then
    echo
    echo "$(L "Gidermek için (sırayla çalıştırın):" "To fix (run in order):")"
    for c in "${fix[@]}"; do printf '    %s\n' "$c"; done
  fi
  echo
  echo "$(L "Sonra yeniden çalıştırın:  sudo ./setup.sh" "Then run again:  sudo ./setup.sh")"
  trap - ERR
  exit 1
fi
ok "$(L "Gereksinimler tamam" "Prerequisites OK") (Docker $(docker version --format '{{.Server.Version}}'), Compose ${compose_v#v}, Buildx ${buildx_v#v})."

# ── 2) Questions (first run) ─────────────────────────────────────────────────
valid_host() {
  local h="$1"
  [ ${#h} -le 253 ] || return 1
  [[ "$h" =~ ^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$ ]] || return 1
  [[ ! "${h##*.}" =~ ^[0-9]+$ ]]       # the last label is not a number (that is an IP)
}
valid_ipv4() {
  local ip="$1" IFS=. o
  [[ "$ip" =~ ^[0-9]{1,3}(\.[0-9]{1,3}){3}$ ]] || return 1
  for o in $ip; do [ "$((10#$o))" -le 255 ] || return 1; done
  [[ "$ip" != 0.* && "$ip" != 127.* && "$ip" != 255.255.255.255 ]]
}
valid_user() { [[ "$1" =~ ^[[:alnum:]._@-]{1,64}$ ]]; }

ask() { # ask VAR "question" default
  local __v="$1" q="$2" def="${3:-}" ans
  if [ -n "$def" ]; then read -r -p "  $q [$def]: " ans || true; else read -r -p "  $q: " ans || true; fi
  printf -v "$__v" '%s' "${ans:-$def}"
}

# read_password_file PATH — the first line of a regular file that only root can
# read. Sets ADMIN_PASS.
read_password_file() {
  local f="$1" st mode
  local what="TOLIKOPA_ADMIN_PASSWORD_FILE ($f)"
  { [ -f "$f" ] && [ ! -L "$f" ]; } || die "$what: $(L "normal bir dosya bulunamadı" "not found or not a regular file")"
  st="$(stat -c '%u %a' -- "$f")"
  [ "${st%% *}" = 0 ] || die "$what: $(L "dosyanın sahibi root olmalı:  sudo chown root:root $f" "the file must be owned by root:  sudo chown root:root $f")"
  mode="${st#* }"
  [ $(( 8#$mode & 8#077 )) -eq 0 ] \
    || die "$what: $(L "dosyayı yalnızca root okuyabilmeli (izin $mode):  sudo chmod 600 $f" "only root may be able to read the file (mode $mode):  sudo chmod 600 $f")"
  ADMIN_PASS=
  IFS= read -r ADMIN_PASS < "$f" || true
  ADMIN_PASS="${ADMIN_PASS%$'\r'}"
}

ask_admin() {
  local pass_src=TOLIKOPA_ADMIN_PASSWORD
  ADMIN_USER="${TOLIKOPA_ADMIN_USER:-}"
  ADMIN_PASS="${TOLIKOPA_ADMIN_PASSWORD:-}"
  if [ -n "${TOLIKOPA_ADMIN_PASSWORD_FILE:-}" ]; then
    read_password_file "$TOLIKOPA_ADMIN_PASSWORD_FILE"
    pass_src=TOLIKOPA_ADMIN_PASSWORD_FILE
    check_password "$ADMIN_PASS" || die "$pass_src: $PASS_ERR"
  fi
  # Not inherited by anything this script starts (docker compose, curl, ...).
  unset TOLIKOPA_ADMIN_PASSWORD
  if [ -n "$ADMIN_USER" ] && [ -n "$ADMIN_PASS" ]; then
    valid_user "$ADMIN_USER" || die "TOLIKOPA_ADMIN_USER: $(L "geçersiz kullanıcı adı" "invalid username")"
    check_password "$ADMIN_PASS" || die "$pass_src: $PASS_ERR"
    return
  fi
  [ -t 0 ] || die "$(L "Soruları sormak için bir terminal gerekli (veya TOLIKOPA_* ortam değişkenlerini verin)." "A terminal is needed to ask the questions (or set the TOLIKOPA_* environment variables).")"
  echo
  echo "$(L "İlk yönetici hesabı (panele bu hesapla girilecek):" "First administrator account (you sign in to the panel with it):")"
  while :; do
    ask ADMIN_USER "$(L "Kullanıcı adı" "Username")" "admin"
    valid_user "$ADMIN_USER" && break
    echo "    $(L "Harf, rakam ve . _ - @ kullanılabilir (en çok 64)." "Letters, digits and . _ - @ (at most 64).")"
  done
  # A password that came from TOLIKOPA_ADMIN_PASSWORD_FILE is already checked.
  [ "$pass_src" = TOLIKOPA_ADMIN_PASSWORD_FILE ] && return
  while :; do
    IFS= read -r -s -p "  $(L "Parola (en az 10 karakter)" "Password (at least 10 characters)"): " ADMIN_PASS || true; echo
    if ! check_password "$ADMIN_PASS"; then echo "    $PASS_ERR"; continue; fi
    IFS= read -r -s -p "  $(L "Parola (tekrar)" "Password (again)"): " p2 || true; echo
    [ "$ADMIN_PASS" = "$p2" ] && break
    echo "    $(L "Parolalar eşleşmiyor." "The passwords do not match.")"
  done
  unset p2
}
check_password() {
  local pw="$1" nbytes
  if [ "${#pw}" -lt 10 ]; then PASS_ERR="$(L "En az 10 karakter olmalı." "Must be at least 10 characters.")"; return 1; fi
  nbytes=$(printf '%s' "$pw" | LC_ALL=C wc -c)
  if [ "$nbytes" -gt 72 ]; then PASS_ERR="$(L "En çok 72 bayt olabilir." "Must be at most 72 bytes.")"; return 1; fi
  return 0
}

detect_ip() {
  { ip route get 1.1.1.1 2>/dev/null || true; } | awk '{for(i=1;i<=NF;i++) if($i=="src"){print $(i+1); exit}}'
}

FIRST_RUN=0
if [ -f "$ENV_FILE" ]; then
  say "$(L "Mevcut kurulum bulundu (deploy/.env) — güncelleniyor, soru sorulmayacak." "Existing installation found (deploy/.env) — updating, no questions.")"
  read -r PANEL_HOST SERVER_IP < <(sed -n 's/^SERVER_HOST="\{0,1\}\([^"]*\)"\{0,1\}$/\1/p' "$ENV_FILE" | head -n1) || true
  [ -n "${PANEL_HOST:-}" ] || die "$(L "deploy/.env içinde SERVER_HOST yok." "SERVER_HOST is missing from deploy/.env.")"
  # The certificate is created before .env is written, so an .env without it was
  # not left behind by this script: the certificate was lost or not restored.
  if [ ! -f "$CERT" ] || [ ! -f "$KEY" ]; then
    die "$(L "deploy/tls içinde sertifika yok. Yedekten geri koyun; yeni sertifika üretmek, bilgisayarlara dağıtılmış güveni ve ajanların sabitlediği sertifikayı geçersiz kılar. (setup.sh'nin eski bir sürümünün yarıda kalmış ilk kurulumuysa ve hiç ajan dağıtılmadıysa: deploy/.env dosyasını silip yeniden çalıştırın.)" "No certificate in deploy/tls. Restore it from a backup: a new certificate invalidates the trust distributed to the PCs and the one the agents pin. (If this is an interrupted first run of an older setup.sh and no agent was ever deployed: delete deploy/.env and run again.)")"
  fi
else
  FIRST_RUN=1
  PANEL_HOST="${TOLIKOPA_HOST:-}"
  SERVER_IP="${TOLIKOPA_IP:-}"
  if [ -z "$PANEL_HOST" ] || [ -z "$SERVER_IP" ]; then
    [ -t 0 ] || die "$(L "Soruları sormak için bir terminal gerekli (veya TOLIKOPA_* ortam değişkenlerini verin)." "A terminal is needed to ask the questions (or set the TOLIKOPA_* environment variables).")"
    echo
    echo "$(L "Panelin adresi. Bilgisayarlar ve tarayıcılar sunucuya bu adla ulaşacak; adı daha sonra tolikopa-setup.exe etki alanının DNS'ine ekler." "The panel's address. PCs and browsers reach the server by this name; tolikopa-setup.exe later adds it to the domain's DNS.")"
    echo "$(L "Etki alanınız örneğin corp.local ise tolikopa.corp.local iyi bir seçimdir." "If your domain is, say, corp.local, tolikopa.corp.local is a good choice.")"
    def_host="$PANEL_HOST"
    while :; do
      ask PANEL_HOST "$(L "Panel DNS adı" "Panel DNS name")" "$def_host"
      PANEL_HOST="$(printf '%s' "$PANEL_HOST" | tr '[:upper:]' '[:lower:]')"
      valid_host "$PANEL_HOST" && break
      echo "    $(L "tolikopa.corp.local gibi tam bir ad girin." "Enter a full name such as tolikopa.corp.local.")"
    done
    def_ip="${SERVER_IP:-$(detect_ip)}"
    while :; do
      ask SERVER_IP "$(L "Bu sunucunun IP adresi" "This server's IP address")" "$def_ip"
      valid_ipv4 "$SERVER_IP" && break
      echo "    $(L "10.0.0.10 gibi bir IPv4 adresi girin." "Enter an IPv4 address such as 10.0.0.10.")"
    done
  fi
  PANEL_HOST="$(printf '%s' "$PANEL_HOST" | tr '[:upper:]' '[:lower:]')"
  valid_host "$PANEL_HOST" || die "$(L "Geçersiz DNS adı" "Invalid DNS name"): $PANEL_HOST"
  valid_ipv4 "$SERVER_IP" || die "$(L "Geçersiz IP adresi" "Invalid IP address"): $SERVER_IP"
  if ! ip -4 -o addr show | awk '{print $4}' | cut -d/ -f1 | grep -qx "$SERVER_IP"; then
    warn "$(L "$SERVER_IP bu sunucunun bir arayüzünde tanımlı değil (NAT arkasındaysa doğru olabilir)." "$SERVER_IP is not assigned to any interface of this server (fine behind NAT).")"
  fi
  ask_admin

  echo
  echo "$(L "Özet:" "Summary:")"
  echo "  Panel:     https://$PANEL_HOST   (https://$SERVER_IP)"
  echo "  $(L "Yönetici" "Admin"):  $ADMIN_USER"
  if [ -t 0 ] && [ -z "${TOLIKOPA_HOST:-}" ]; then
    read -r -p "  $(L "Devam edilsin mi? [E/h]" "Continue? [Y/n]"): " go || true
    case "${go:-}" in [hHnN]*) echo "$(L "İptal edildi; hiçbir şey değiştirilmedi." "Cancelled; nothing was changed.")"; trap - ERR; exit 0 ;; esac
  fi
fi

# ── 3) Certificate, then secrets (first run only) ────────────────────────────
# Order matters: .env is the marker of an existing installation, and an existing
# installation without a certificate is treated as "restore it from a backup".
# Writing .env last means a run that dies half-way is simply a first run again.
if [ "$FIRST_RUN" -eq 1 ]; then
  umask 077
  if [ -f "$CERT" ] && [ -f "$KEY" ]; then
    # Left by an interrupted first run, or restored from a backup before .env was:
    # reuse it (never replace a certificate that may already be distributed), but
    # only if it is for the names given now.
    if ! openssl x509 -in "$CERT" -noout -checkhost "$PANEL_HOST" 2>/dev/null | grep -q 'does match' \
       || ! openssl x509 -in "$CERT" -noout -checkip "$SERVER_IP" 2>/dev/null | grep -q 'does match'; then
      die "$(L "deploy/tls içinde $PANEL_HOST / $SERVER_IP için olmayan bir sertifika var. Yedekten geri konduysa aynı adları girin; yarıda kalmış bir kurulumdan kaldıysa ve hiç dağıtılmadıysa deploy/tls klasörünü silip yeniden çalıştırın." "deploy/tls holds a certificate that is not for $PANEL_HOST / $SERVER_IP. If it was restored from a backup, enter the same names; if it is left over from an interrupted install and was never distributed, delete deploy/tls and run again.")"
    fi
    ok "$(L "Mevcut sertifika kullanılıyor" "Using the existing certificate"): $CERT"
  else
    say "$(L "Kendinden imzalı sertifika oluşturuluyor: $PANEL_HOST, $SERVER_IP ($CERT_DAYS gün)..." "Creating a self-signed certificate: $PANEL_HOST, $SERVER_IP ($CERT_DAYS days)...")"
    install -d -m 0700 "$TLS_DIR"
    cnf="$(mktemp)"
    # A server certificate, not a CA: trusted as a root on the PCs it can vouch for
    # nothing but these two names, so a stolen key cannot impersonate other sites.
    cat > "$cnf" <<EOF
[req]
distinguished_name = dn
prompt = no
x509_extensions = ext
[dn]
CN = $PANEL_HOST
O = Tolikopa
[ext]
basicConstraints = critical, CA:FALSE
keyUsage = critical, digitalSignature
extendedKeyUsage = serverAuth
subjectAltName = DNS:$PANEL_HOST, IP:$SERVER_IP
subjectKeyIdentifier = hash
EOF
    openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -days "$CERT_DAYS" \
      -config "$cnf" -keyout "$KEY.tmp" -out "$CERT.tmp" 2>/dev/null
    rm -f "$cnf"
    chmod 600 "$KEY.tmp"; chmod 644 "$CERT.tmp"
    mv "$KEY.tmp" "$KEY"; mv "$CERT.tmp" "$CERT"
    ok "$(L "Sertifika hazır" "Certificate ready"): $CERT"
  fi

  say "$(L "Gizli anahtarlar üretiliyor (deploy/.env)..." "Generating secrets (deploy/.env)...")"
  # Plain assignments, not $(...) inside the here-document: there a failing
  # openssl would not stop the script and .env would get empty secrets.
  pg_pass="$(openssl rand -hex 24)"
  ingest_token="$(openssl rand -hex 32)"
  jwt_secret="$(openssl rand -hex 32)"
  { [ ${#pg_pass} -eq 48 ] && [ ${#ingest_token} -eq 64 ] && [ ${#jwt_secret} -eq 64 ]; } \
    || die "$(L "openssl rand beklenen çıktıyı vermedi." "openssl rand did not produce the expected output.")"
  cat > "$ENV_FILE.tmp" <<EOF
# Tolikopa — created by setup.sh on $(date -u +%Y-%m-%dT%H:%M:%SZ). Keep secret; never commit.
POSTGRES_USER=tolikopa
POSTGRES_DB=tolikopa
POSTGRES_PASSWORD=$pg_pass
INGEST_TOKEN=$ingest_token
JWT_SECRET=$jwt_secret
SETUP_CODE=
SERVER_HOST="$PANEL_HOST $SERVER_IP"
DEFAULT_SNI=$PANEL_HOST
OFFLINE_WARN_SEC=300
OFFLINE_CRIT_SEC=1800
TZ=Europe/Istanbul
SMTP_ENABLED=false
SMTP_HOST=
SMTP_PORT=587
SMTP_USER=
SMTP_PASS=
SMTP_FROM=
ALERT_EMAIL_TO=
EOF
  chmod 600 "$ENV_FILE.tmp"
  mv "$ENV_FILE.tmp" "$ENV_FILE"
  unset pg_pass ingest_token jwt_secret
  ok "$(L ".env oluşturuldu (izin 600)." ".env created (mode 600).")"
fi

# Caddy runs with every capability dropped except NET_BIND_SERVICE, so it reads
# the key as its owner: root, mode 600 (also repairs a backup restored by another
# user). The certificate is public (agents and the api container read it).
chown 0:0 "$KEY" "$CERT"
chmod 600 "$KEY"; chmod 644 "$CERT"

# ── 4) Firewall ──────────────────────────────────────────────────────────────
if command -v ufw >/dev/null 2>&1 && ufw status 2>/dev/null | grep -q '^Status: active'; then
  ufw allow 80/tcp >/dev/null && ufw allow 443/tcp >/dev/null
  ok "$(L "ufw: 80 ve 443 açık." "ufw: 80 and 443 allowed.")"
  # Docker's own iptables rules for published ports come before ufw's.
  echo "  ${c_d}$(L "Not: Docker'ın yayımladığı portlar ufw kurallarını atlar; ufw ile kısıtlanamaz. Tek bir arayüzü dinlemek için deploy/.env içinde BIND_ADDR=<IP> verin." "Note: ports published by Docker bypass ufw rules; ufw cannot restrict them. To listen on one interface only, set BIND_ADDR=<IP> in deploy/.env.")${c_0}"
fi

# ── 5) Build and start ───────────────────────────────────────────────────────
say "$(L "İmajlar derleniyor ve servisler başlatılıyor (ilk seferde 5–15 dakika sürebilir)..." "Building images and starting services (5–15 minutes the first time)...")"
dc up -d --build --remove-orphans
ok "$(L "Container'lar çalışıyor." "Containers are running.")"

say "$(L "Panelin yanıt vermesi bekleniyor..." "Waiting for the panel to answer...")"
# With BIND_ADDR set in .env, 443 is published on that address only, not loopback.
probe_ip="$(sed -n 's/^BIND_ADDR=["'\'']\{0,1\}\([^"'\'']*\)["'\'']\{0,1\}$/\1/p' "$ENV_FILE" | tail -n1)"
case "$probe_ip" in ""|0.0.0.0) probe_ip=127.0.0.1 ;; esac
healthy=0
for _ in $(seq 1 100); do
  # Through Caddy, by name, verified against our own certificate: proves DNS-free
  # TLS, the proxy and the API in one request.
  if curl -fsS --max-time 5 --resolve "$PANEL_HOST:443:$probe_ip" --cacert "$CERT" \
       "https://$PANEL_HOST/api/healthz" >/dev/null 2>&1; then
    healthy=1; break
  fi
  sleep 3
done
if [ "$healthy" -ne 1 ]; then
  die "$(L "Panel 5 dakikada yanıt vermedi. Günlüklere bakın:" "The panel did not answer within 5 minutes. Check the logs:")  sudo docker compose -f $DEPLOY_DIR/docker-compose.yml logs api web"
fi
ok "$(L "Panel yanıt veriyor (TLS doğrulandı)." "The panel answers (TLS verified).")"

# ── 6) First administrator ───────────────────────────────────────────────────
needs_admin="$(curl -fsS --max-time 5 --resolve "$PANEL_HOST:443:$probe_ip" --cacert "$CERT" \
  "https://$PANEL_HOST/api/setup/status" | grep -o '"needs_admin":[a-z]*' | cut -d: -f2 || true)"
if [ "$needs_admin" = true ]; then
  [ -n "${ADMIN_USER:-}" ] || ask_admin
  say "$(L "Yönetici hesabı oluşturuluyor: $ADMIN_USER" "Creating the administrator account: $ADMIN_USER")"
  # The password goes through stdin: never on a command line, in .env or in a log.
  if ! out="$(printf '%s\n' "$ADMIN_PASS" | dc exec -T api /server admin init-admin "$ADMIN_USER" 2>&1)"; then
    unset ADMIN_PASS
    die "$(L "Yönetici oluşturulamadı" "Could not create the administrator"): $out"
  fi
  unset ADMIN_PASS
  ok "$(L "Yönetici hesabı oluşturuldu." "Administrator account created.")"
elif [ "$needs_admin" = false ]; then
  [ "$FIRST_RUN" -eq 0 ] || warn "$(L "Veritabanında zaten bir yönetici var; yeni hesap oluşturulmadı." "The database already has an administrator; no new account was created.")"
  unset ADMIN_PASS
else
  warn "$(L "Yönetici durumu okunamadı; panelde ilk kurulum ekranı çıkarsa kurulum kodu: sudo docker compose -f $DEPLOY_DIR/docker-compose.yml logs api | grep -i 'setup code'" "Could not read the admin status; if the panel shows the first-run screen, the setup code is in: sudo docker compose -f $DEPLOY_DIR/docker-compose.yml logs api | grep -i 'setup code'")"
fi

# ── 7) Linux agent (the server monitors itself) ──────────────────────────────
say "$(L "Linux ajanı kuruluyor (sunucunun kendini izlemesi için)..." "Installing the Linux agent (the server monitors itself)...")"
agent_tmp="$(mktemp)"
if dc cp api:/opt/tolikopa/artifacts/tolikopa-agent "$agent_tmp" >/dev/null 2>&1; then
  systemctl stop tolikopa-agent >/dev/null 2>&1 || true
  install -D -m 0755 "$agent_tmp" /opt/tolikopa/tolikopa-agent
  install -d -m 0700 /etc/tolikopa
  # The agent's only writable place under the unit's ProtectSystem=strict (state,
  # spool, logs). It must exist before the unit starts: ReadWritePaths= fails on a
  # missing path.
  install -d -m 0755 /var/lib/tolikopa
  token="$(sed -n 's/^INGEST_TOKEN=//p' "$ENV_FILE" | head -n1)"
  # PEM is base64 + header lines: escaping the line breaks is all JSON needs.
  pem_json="$(awk '{printf "%s\\n", $0}' "$CERT")"
  umask 077
  cat > /etc/tolikopa/config.json <<EOF
{
  "backend_url": "https://$SERVER_IP",
  "ingest_token": "$token",
  "tls_ca_pem": "$pem_json",
  "log_level": "info"
}
EOF
  chmod 600 /etc/tolikopa/config.json
  cat > /etc/systemd/system/tolikopa-agent.service <<'UNIT'
[Unit]
Description=Tolikopa Agent (self-monitoring)
After=network-online.target docker.service
Wants=network-online.target

[Service]
ExecStart=/opt/tolikopa/tolikopa-agent run
Restart=always
RestartSec=10
# Runs as root (it reads every process's /proc entry and statfs()s every mount),
# but sees the file system read-only except its own /var/lib/tolikopa (state,
# spool, logs). ProtectHome=read-only, not yes: disk usage of a /home mount still
# needs the mount point visible. /proc, /sys and /run/systemd/sessions stay readable.
ProtectSystem=strict
ProtectHome=read-only
PrivateTmp=yes
ReadWritePaths=/var/lib/tolikopa
NoNewPrivileges=yes

[Install]
WantedBy=multi-user.target
UNIT
  systemctl daemon-reload
  systemctl enable tolikopa-agent >/dev/null 2>&1
  systemctl restart tolikopa-agent
  ok "$(L "Linux ajanı çalışıyor (systemd: tolikopa-agent)." "The Linux agent is running (systemd: tolikopa-agent).")"
else
  warn "$(L "Linux ajanı imajdan alınamadı; panel yine de çalışıyor. setup.sh'yi yeniden çalıştırabilirsiniz." "Could not take the Linux agent from the image; the panel works regardless. You can run setup.sh again.")"
fi
rm -f "$agent_tmp"

# ── 8) Summary ───────────────────────────────────────────────────────────────
fp="$(openssl x509 -in "$CERT" -noout -fingerprint -sha256 | cut -d= -f2)"
until_date="$(openssl x509 -in "$CERT" -noout -enddate | cut -d= -f2)"
trap - ERR
echo
echo "────────────────────────────────────────────────────────────"
ok "$(L "Kurulum tamamlandı." "Setup complete.")"
echo
echo "  Panel:      ${c_g}https://$PANEL_HOST${c_0}"
echo "              https://$SERVER_IP  $(L "(DNS kaydı oluşana kadar bunu kullanın)" "(use this until the DNS record exists)")"
echo "  $(L "Sertifika" "Certificate"):  SHA-256 $fp"
echo "              $(L "geçerlilik sonu" "valid until"): $until_date"
echo
echo "$(L "Sıradaki adımlar:" "Next steps:")"
echo "  1. $(L "Panele girin. Tarayıcı sertifika uyarısı verecek: parmak izi yukarıdakiyle aynıysa devam edin." "Open the panel. The browser warns about the certificate: continue if the fingerprint matches the one above.")"
echo "  2. $(L "Kurulum sihirbazında etki alanı bilgilerini girip tolikopa-setup.zip dosyasını indirin ve tek bir klasöre çıkarın." "In the setup wizard enter the domain details, download tolikopa-setup.zip and extract it into one folder.")"
echo "  3. $(L "Aracı etki alanına bağlı bir yönetici bilgisayarında çalıştırın: ajanı dağıtır, $PANEL_HOST DNS kaydını oluşturur ve bu sertifikayı GPO ile bilgisayarlara güvenilir yapar." "Run it on a domain-joined admin PC: it deploys the agent, creates the $PANEL_HOST DNS record and makes the PCs trust this certificate through the GPO.")"
echo
echo "  $(L "Güncelleme" "Update"):  cd $REPO_ROOT && git pull && sudo ./setup.sh"
echo "  ${c_d}$(L "deploy/.env ve deploy/tls/ yedeklenmeli: sertifika kaybolursa bilgisayarlara yeniden dağıtılması gerekir." "Back up deploy/.env and deploy/tls/: if the certificate is lost it must be distributed to the PCs again.")${c_0}"
echo
