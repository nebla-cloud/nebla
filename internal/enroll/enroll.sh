#!/bin/bash

# Copyright (C) 2026 The Nebla contributors
#
# This program is free software: you can redistribute it and/or modify
# it under the terms of the GNU Affero General Public License as published by
# the Free Software Foundation, either version 3 of the License, or
# (at your option) any later version.
#
# This program is distributed in the hope that it will be useful,
# but WITHOUT ANY WARRANTY; without even the implied warranty of
# MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
# GNU Affero General Public License for more details.
#
# You should have received a copy of the GNU Affero General Public License
# along with this program.  If not, see <https://www.gnu.org/licenses/>.

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
re_sdn_prefix='^[a-z][a-z0-9]{0,3}$'
re_sdn_zone='^[a-z][a-z0-9]{0,7}$'
re_sdn_count='^[0-9]{1,3}$'

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

# Creates the private network zone and VNets that are missing, then applies SDN once. Uses the caller's
# $cluster, $nodes, $sdn_vnets, $sdn_zone and $sdn_prefix, and sets $sdn_msg for the summary.
setup_sdn() {
    local zones vnets line ztype="" ips="" ip i name changed=0 made=0 have=0 nn=0 n
    zones=$(pvesh get /cluster/sdn/zones --output-format json)
    vnets=$(pvesh get /cluster/sdn/vnets --output-format json)
    if has_val "$zones" zone "$sdn_zone"; then
        while IFS= read -r line; do
            [ "$(field "$line" zone)" = "$sdn_zone" ] && ztype=$(field "$line" type)
        done < <(printf '%s\n' "$zones" | tr '{}' '\n\n')
        say "private network zone $sdn_zone exists"
    else
        for n in $nodes; do nn=$((nn + 1)); done
        if [ "$nn" -le 1 ]; then
            ztype=simple
            pvesh create /cluster/sdn/zones --zone "$sdn_zone" --type simple --ipam pve
        else
            ztype=vxlan
            while IFS= read -r line; do
                [[ $line =~ $re_type_node ]] || continue
                ip=$(field "$line" ip)
                [ -n "$ip" ] || die "node $(field "$line" name) has no IP in the cluster status; can't create a VXLAN zone"
                ips="$ips${ips:+,}$ip"
            done < <(printf '%s\n' "$cluster" | tr '{}' '\n\n')
            pvesh create /cluster/sdn/zones --zone "$sdn_zone" --type vxlan --peers "$ips"
        fi
        changed=1
        say "created private network zone $sdn_zone ($ztype)"
    fi
    for ((i = 1; i <= sdn_vnets; i++)); do
        printf -v name '%s%04d' "$sdn_prefix" "$i"
        if has_val "$vnets" vnet "$name"; then
            have=$((have + 1))
            continue
        fi
        pvesh create /cluster/sdn/vnets --vnet "$name" --zone "$sdn_zone"
        made=$((made + 1))
        changed=1
    done
    say "private networks: $made new, $have already there"
    if [ "$changed" = 1 ]; then
        pvesh set /cluster/sdn
        say "applied SDN"
    fi
    sdn_msg="private networks: zone $sdn_zone, $sdn_vnets VNets"
    if [ "$ztype" = vxlan ]; then
        if [ -z "$ips" ]; then
            while IFS= read -r line; do
                [[ $line =~ $re_type_node ]] || continue
                ips="$ips${ips:+,}$(field "$line" ip)"
            done < <(printf '%s\n' "$cluster" | tr '{}' '\n\n')
        fi
        say "VXLAN needs a host firewall rule on every node (datacenter firewall on): IN ACCEPT -source $ips -p udp -dport 4789"
        say "Accept UDP 4789 from the cluster nodes only; see docs/proxmox-setup.md"
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
    local t tools pv ver token_name rc resp code body backup=""
    local sdn_vnets=${NEBLA_SDN_VNETS:-} sdn_zone=${NEBLA_SDN_ZONE:-nebla} sdn_prefix=${NEBLA_SDN_PREFIX:-nb} sdn_msg=""
    while [ $# -gt 0 ]; do
        case $1 in
        --backup-storage=*)
            backup=${1#*=}
            shift
            ;;
        --backup-storage)
            [ $# -ge 2 ] || die "--backup-storage needs a storage id"
            backup=$2
            shift 2
            ;;
        *) die "unknown option '$1'" ;;
        esac
    done
    if [ -n "$backup" ] && ! [[ $backup =~ $re_storage ]]; then
        die "unexpected storage id '$backup'"
    fi

    [ "$(id -u)" = 0 ] || die "run this as root"
    tools="pveum pvesh pveversion openssl base64"
    [ -n "$offline" ] || tools="$tools curl"
    for t in $tools; do
        command -v "$t" >/dev/null 2>&1 || die "$t not found; run this on a Proxmox VE node"
    done
    if [ -n "$sdn_vnets" ]; then
        [[ $sdn_vnets =~ $re_sdn_count ]] && [ $((10#$sdn_vnets)) -ge 1 ] && [ $((10#$sdn_vnets)) -le 200 ] ||
            die "NEBLA_SDN_VNETS must be a number from 1 to 200"
        sdn_vnets=$((10#$sdn_vnets))
        [[ $sdn_prefix =~ $re_sdn_prefix ]] || die "NEBLA_SDN_PREFIX must be 1 to 4 lowercase letters or digits, starting with a letter"
        [[ $sdn_zone =~ $re_sdn_zone ]] || die "NEBLA_SDN_ZONE must be 1 to 8 lowercase letters or digits, starting with a letter"
    fi
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

    if [ -n "$backup" ]; then
        local bcfg
        bcfg=$(pvesh get "/storage/$backup" --output-format json) || die "storage $backup not found"
        [ "$(field "$bcfg" type)" = pbs ] || die "$backup is not a Proxmox Backup Server storage"
    fi

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
        privs="VM.Allocate VM.Clone VM.Config.CDROM VM.Config.CPU VM.Config.Disk VM.Config.Memory VM.Config.Network VM.Config.Options VM.Config.Cloudinit VM.PowerMgmt VM.Console VM.Backup VM.Audit VM.Monitor Datastore.AllocateSpace Datastore.AllocateTemplate Datastore.Audit Sys.Audit SDN.Use SDN.Audit"
    else
        privs="VM.Allocate VM.Clone VM.Config.CDROM VM.Config.CPU VM.Config.Disk VM.Config.Memory VM.Config.Network VM.Config.Options VM.Config.Cloudinit VM.PowerMgmt VM.Console VM.Backup VM.Audit VM.GuestAgent.Audit Datastore.AllocateSpace Datastore.AllocateTemplate Datastore.Audit Sys.Audit SDN.Use SDN.Audit"
    fi
    admin=$(pvesh get /access/roles/Administrator --output-format json)
    case $admin in *'"Sys.AccessNetwork"'*) privs="$privs Sys.AccessNetwork" ;; esac
    case $admin in *'"VM.Config.HWType"'*) privs="$privs VM.Config.HWType" ;; esac
    roles=$(pveum role list --output-format json)
    set_role Nebla "$privs" "$roles"
    set_role NeblaISO "Datastore.Allocate Datastore.AllocateSpace Datastore.AllocateTemplate Datastore.Audit" "$roles"
    if [ -n "$backup" ]; then
        set_role NeblaBackup "Datastore.AllocateSpace Datastore.Audit" "$roles"
    fi

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
    if [ -n "$backup" ]; then
        pveum acl modify "/storage/$backup" --users nebla@pve --roles NeblaBackup
        say "granted NeblaBackup on /storage/$backup"
    fi

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
    if [ -n "$sdn_vnets" ]; then setup_sdn; fi

    local iso_json="" report blob extra=""
    sep=""
    for id in $isos; do
        iso_json="$iso_json$sep\"$id\""
        sep=,
    done
    [ -z "$backup" ] || extra=",\"backup_storage\":\"$backup\""
    report=$(printf '{"v":1,"pve_version":"%s","node":"%s","token":%s,"cluster":%s,"storage":%s,"fingerprints":{%s},"nebla_iso":[%s]%s}' \
        "$ver" "$node" "$tok" "$cluster" "$storage" "$fps" "$iso_json" "$extra")

    if [ -z "$offline" ]; then
        resp=$(printf '%s' "$report" | curl -sS --max-time 20 --config <(auth "$token") -H 'Content-Type: application/json' \
            --data-binary @- -w '\n%{http_code}' "$panel/v1/enroll/report") || true
        code=${resp##*"$nl"}
        body=${resp%"$nl"*}
        case $code in
        204)
            say "sent the report to the panel"
            summary "$token_name" "$isos" "$sdn_msg"
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
    summary "$token_name" "$isos" "$sdn_msg"
    blob=$(printf '%s,"enroll_token":"%s"}' "${report%\}}" "$token" | base64 | tr -d '\n' | tr '/+' '_-' | tr -d '=')
    printf 'nebla-enroll:v1:%s\n' "$blob"
    say "Paste this line into the panel. It contains a secret; clear your scrollback afterwards." >&2
}

summary() {
    local isos=${2# }
    say "token: nebla@pve!$1"
    say "NeblaISO on: ${isos:-no storage}"
    [ -z "${3:-}" ] || say "$3"
    say "Finish in the panel."
}

main "$@" </dev/null
