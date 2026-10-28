#!/usr/bin/env bash

# Select the initialization mode without rewriting existing database files.
# NODE_IPS also feeds the leader checker; an empty list selects standalone there.
configure_ovn_db_mode() {
    local db_dir="$1"
    NODE_IPS=${NODE_IPS//[[:space:]]/}
    if [[ "$NODE_IPS" == ,* || "$NODE_IPS" == *, || "$NODE_IPS" == *,,* ]]; then
        echo "ERROR! NODE_IPS contains an empty address: $NODE_IPS" >&2
        return 1
    fi

    # Keep the existing multi-replica startup and recovery behavior.
    if [[ "$NODE_IPS" == *,* ]]; then
        return 0
    fi

    local clustered=false
    local standalone=false
    local db_file
    for db_file in "$db_dir/ovnnb_db.db" "$db_dir/ovnsb_db.db"; do
        if [[ -e "${db_file%.db}.hdr" ]]; then
            clustered=true
        fi
        if [[ -e "$db_file" ]]; then
            if ovsdb-tool db-is-clustered "$db_file"; then
                clustered=true
            elif ovsdb-tool db-is-standalone "$db_file"; then
                standalone=true
            else
                echo "ERROR! cannot determine the storage model of $db_file" >&2
                return 1
            fi
        fi
    done
    if [[ "$clustered" == true && "$standalone" == true ]]; then
        echo "ERROR! NB/SB have mixed storage models; explicit recovery is required" >&2
        return 1
    fi

    if [[ "$clustered" == true ]]; then
        # A replica count change is not a database migration. A surviving
        # member of a larger cluster must not bootstrap an independent DB.
        if [[ -z "$NODE_IPS" ]]; then
            echo "ERROR! existing Raft data requires its Raft addresses or explicit migration to standalone" >&2
            return 1
        fi
        echo "Preserving existing Raft data; explicit migration is required for standalone" >&2
    else
        NODE_IPS=""
    fi
}
