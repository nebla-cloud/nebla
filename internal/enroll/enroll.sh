#!/bin/bash
# Nebla: connect this Proxmox VE cluster to the panel. Run as root on any node.
set -euo pipefail

PANEL_URL='{{.PanelURL}}'

nl=$'\n'
re_token='^nbe_[A-Za-z0-9_-]{43}$'
re_name='^nebla-[a-z0-9-]{1,32}-[a-z0-9]{6}$'
re_ver='^[0-9]+\.[0-9][A-Za-z0-9.~+-]*$'
re_node='^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?$'
re_storage='^[A-Za-z][A-Za-z0-9._-]{0,99}$'
re_uuid='^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
re_fp='^[0-9A-F:]{95}$'
re_type_node='"type":[[:space:]]*"node"'
re_local='"local":[[:space:]]*"?1"?([^0-9]|$)'

say() { printf 'nebla: %s\n' "$*"; }
warn() { printf 'nebla: warning: %s\n' "$*" >&2; }
die() {
    printf 'nebla: error: %s\n' "$*" >&2
    exit 1
}

made_token=""
on_exit() {
    local rc=$?
    if [ "$rc" != 0 ] && [ -n "$made_token" ]; then
        printf 'nebla: remove the new token with: pveum user token remove nebla@pve %s\n' "$made_token" >&2
    fi
}
trap on_exit EXIT

# Passes the auth header through a curl config on a pipe, so the token stays out of argv.
auth() { printf 'header = "Authorization: Enroll %s"\n' "$1"; }

# Prints field $2 of each flat object in JSON $1, one per line.
vals() { printf '%s\n' "$1" | tr '{}' '\n\n' | sed -n "s/.*\"$2\":[[:space:]]*\"\([^\"]*\)\".*/\1/p"; }

has_val() {
    case "$nl$(vals "$1" "$2")$nl" in
    *"$nl$3$nl"*) return 0 ;;
    esac
    return 1
}

field() { printf '%s\n' "$1" | sed -n "s/.*\"$2\":[[:space:]]*\"\([^\"]*\)\".*/\1/p"; }

panel_msg() {
    local m
    m=$(field "$1" message)
    printf '%s' "${m:-HTTP $2}"
}

set_role() {
    if has_val "$3" roleid "$1"; then
        pveum role modify "$1" --privs "$2"
        say "updated role $1"
    else
        pveum role add "$1" --privs "$2"
        say "created role $1"
    fi
}

# Grants NeblaISO on every storage with images content, so volumes can be deleted; the panel offers to
# remove the grant from storages the location doesn't use. Appends to the caller's $isos.
grant_volume_storages() {
    local line id content
    while IFS= read -r line; do
        id=$(field "$line" storage)
        content=$(field "$line" content)
        case ",$content," in
        *,images,*) ;;
        *) continue ;;
        esac
        case " $isos " in *" $id "*) continue ;; esac
        if ! [[ $id =~ $re_storage ]]; then
            warn "skipping storage '$id': unexpected id"
            continue
        fi
        pveum acl modify "/storage/$id" --users nebla@pve --roles NeblaISO
        isos="$isos $id"
        say "granted NeblaISO on /storage/$id (volumes)"
    done < <(printf '%s\n' "$1" | tr '{}' '\n\n')
}

main() {
    # NEBLA_PVE_DIR exists only for tests.
    local pve_dir=${NEBLA_PVE_DIR:-/etc/pve}
    local offline=${NEBLA_OFFLINE:-}
    local panel=${PANEL_URL%/}
    local token=${NEBLA_TOKEN:-}
    local t tools pv ver token_name rc resp code body

    [ "$(id -u)" = 0 ] || die "run this as root"
    tools="pveum pvesh pveversion openssl base64"
    [ -n "$offline" ] || tools="$tools curl"
    for t in $tools; do
        command -v "$t" >/dev/null 2>&1 || die "$t not found; run this on a Proxmox VE node"
    done
    [[ $token =~ $re_token ]] || die "NEBLA_TOKEN is missing or malformed; copy the command from the panel again"
    pv=$(pveversion) || die "pveversion failed"
    ver=${pv#pve-manager/}
    ver=${ver%%/*}
    if [ "${pv#pve-manager/}" = "$pv" ] || [ ${#ver} -gt 64 ] || ! [[ $ver =~ $re_ver ]]; then
        die "Nebla supports Proxmox VE 8 and 9; this node runs an unknown version"
    fi
    case ${ver%%.*} in
    8 | 9) ;;
    *) die "Nebla supports Proxmox VE 8 and 9; this node runs $ver" ;;
    esac
    say "Proxmox VE $ver"

    if [ -n "$offline" ]; then
        token_name=${NEBLA_TOKEN_NAME:-}
    else
        rc=0
        resp=$(curl -sS --max-time 15 --config <(auth "$token") -w '\n%{http_code}' "$panel/v1/enroll/check") || rc=$?
        [ "$rc" = 0 ] || die "can't reach $PANEL_URL; use \"The node can't reach this panel?\" on the enrollment page"
        code=${resp##*"$nl"}
        body=${resp%"$nl"*}
        [ "$code" = 401 ] && die "the panel doesn't know this command; it was finished, cancelled or replaced. Get a new command in the panel"
        [ "$code" = 200 ] || die "the panel refused this enrollment: $(panel_msg "$body" "$code")"
        token_name=$(printf '%s\n' "$body" | sed -n 's/.*"token_name":[[:space:]]*"\(nebla-[a-z0-9-]*\)".*/\1/p')
    fi
    [[ $token_name =~ $re_name ]] || die "the token name is missing or malformed"
    if [ -n "$offline" ]; then
        say "offline mode, token name $token_name"
    else
        say "the panel accepted this enrollment"
    fi

    local users tokens
    users=$(pveum user list --output-format json)
    if has_val "$users" userid nebla@pve; then
        tokens=$(pveum user token list nebla@pve --output-format json)
        if has_val "$tokens" tokenid "$token_name"; then
            die "this setup command was already used; get a new command in the panel"
        fi
    fi

    local privs admin roles
    if [ "${ver%%.*}" = 8 ]; then
        privs="VM.Allocate VM.Clone VM.Config.CDROM VM.Config.CPU VM.Config.Disk VM.Config.Memory VM.Config.Network VM.Config.Options VM.Config.Cloudinit VM.PowerMgmt VM.Console VM.Audit VM.Monitor Datastore.AllocateSpace Datastore.AllocateTemplate Datastore.Audit Sys.Audit SDN.Use"
    else
        privs="VM.Allocate VM.Clone VM.Config.CDROM VM.Config.CPU VM.Config.Disk VM.Config.Memory VM.Config.Network VM.Config.Options VM.Config.Cloudinit VM.PowerMgmt VM.Console VM.Audit VM.GuestAgent.Audit Datastore.AllocateSpace Datastore.AllocateTemplate Datastore.Audit Sys.Audit SDN.Use"
    fi
    admin=$(pvesh get /access/roles/Administrator --output-format json)
    case $admin in *'"Sys.AccessNetwork"'*) privs="$privs Sys.AccessNetwork" ;; esac
    case $admin in *'"VM.Config.HWType"'*) privs="$privs VM.Config.HWType" ;; esac
    roles=$(pveum role list --output-format json)
    set_role Nebla "$privs" "$roles"
    set_role NeblaISO "Datastore.Allocate Datastore.AllocateSpace Datastore.AllocateTemplate Datastore.Audit" "$roles"

    local storage line id content isos=""
    if has_val "$users" userid nebla@pve; then
        say "user nebla@pve exists"
    else
        pveum user add nebla@pve --comment "Nebla panel"
        say "created user nebla@pve"
    fi
    pveum acl modify / --users nebla@pve --roles Nebla
    say "granted Nebla on /"
    storage=$(pvesh get /storage --output-format json)
    while IFS= read -r line; do
        id=$(field "$line" storage)
        content=$(field "$line" content)
        case ",$content," in
        *,iso,* | *,import,*) ;;
        *) continue ;;
        esac
        if ! [[ $id =~ $re_storage ]]; then
            warn "skipping storage '$id': unexpected id"
            continue
        fi
        pveum acl modify "/storage/$id" --users nebla@pve --roles NeblaISO
        isos="$isos $id"
        say "granted NeblaISO on /storage/$id"
    done < <(printf '%s\n' "$storage" | tr '{}' '\n\n')
    grant_volume_storages "$storage"

    local tok secret tid
    tok=$(pveum user token add nebla@pve "$token_name" --privsep 0 --comment "Nebla panel" --output-format json)
    made_token=$token_name
    secret=$(field "$tok" value)
    tid=$(field "$tok" full-tokenid)
    if ! [[ $secret =~ $re_uuid ]] || [ "$tid" != "nebla@pve!$token_name" ]; then
        die "unexpected output from pveum user token add"
    fi
    say "created token nebla@pve!$token_name"

    local cluster node="" nodes="" n pem out f fps="" sep=""
    cluster=$(pvesh get /cluster/status --output-format json)
    while IFS= read -r line; do
        [[ $line =~ $re_type_node ]] || continue
        n=$(field "$line" name)
        [[ $n =~ $re_node ]] || die "unexpected node name '$n' in the cluster status"
        if [[ $line =~ $re_local ]]; then
            node=$n
        fi
        nodes="$nodes $n"
    done < <(printf '%s\n' "$cluster" | tr '{}' '\n\n')
    [ -n "$nodes" ] || die "no nodes in the cluster status"
    node=${node:-${HOSTNAME%%.*}}
    [[ $node =~ $re_node ]] || die "unexpected local node name '$node'"
    for n in $nodes; do
        pem="$pve_dir/nodes/$n/pveproxy-ssl.pem"
        [ -f "$pem" ] || pem="$pve_dir/nodes/$n/pve-ssl.pem"
        out=$(openssl x509 -noout -fingerprint -sha256 -in "$pem") || die "can't read the certificate of node $n ($pem)"
        f=${out#*=}
        [[ $f =~ $re_fp ]] || die "unexpected certificate fingerprint for node $n"
        fps="$fps$sep\"$n\":\"$f\""
        sep=,
    done
    say "read the cluster:$nodes"

    local iso_json="" report blob
    sep=""
    for id in $isos; do
        iso_json="$iso_json$sep\"$id\""
        sep=,
    done
    report=$(printf '{"v":1,"pve_version":"%s","node":"%s","token":%s,"cluster":%s,"storage":%s,"fingerprints":{%s},"nebla_iso":[%s]}' \
        "$ver" "$node" "$tok" "$cluster" "$storage" "$fps" "$iso_json")

    if [ -z "$offline" ]; then
        resp=$(printf '%s' "$report" | curl -sS --max-time 20 --config <(auth "$token") -H 'Content-Type: application/json' \
            --data-binary @- -w '\n%{http_code}' "$panel/v1/enroll/report") || true
        code=${resp##*"$nl"}
        body=${resp%"$nl"*}
        case $code in
        204)
            say "sent the report to the panel"
            summary "$token_name" "$isos"
            return 0
            ;;
        400 | 401 | 409 | 410)
            printf 'nebla: error: the panel refused the report: %s\n' "$(panel_msg "$body" "$code")" >&2
            exit 1
            ;;
        429 | 5??)
            warn "the panel couldn't take the report: $(panel_msg "$body" "$code"); paste it instead"
            ;;
        *) warn "couldn't send the report to the panel (HTTP ${code:-none}); paste it instead" ;;
        esac
    fi
    summary "$token_name" "$isos"
    blob=$(printf '%s,"enroll_token":"%s"}' "${report%\}}" "$token" | base64 | tr -d '\n' | tr '/+' '_-' | tr -d '=')
    printf 'nebla-enroll:v1:%s\n' "$blob"
    say "Paste this line into the panel. It contains a secret; clear your scrollback afterwards." >&2
}

summary() {
    local isos=${2# }
    say "token: nebla@pve!$1"
    say "NeblaISO on: ${isos:-no storage}"
    say "Finish in the panel."
}

main "$@" </dev/null
