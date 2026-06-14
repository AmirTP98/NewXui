#!/usr/bin/env python3
"""
Add a new externalProxy entry (home.wordqress.store) to every inbound
that already has at least one externalProxy configured.

Usage:
  python3 add_external_proxy.py                        # apply
  python3 add_external_proxy.py --dry-run              # preview only
  python3 add_external_proxy.py --rollback             # undo last apply
  python3 add_external_proxy.py --db /path/to/x-ui.db # custom db path
"""

import argparse
import json
import os
import sqlite3
import sys
from datetime import datetime

NEW_DEST = "home.wordqress.store"
BACKUP_FILE = "external_proxy_backup.json"
DEFAULT_DB = "/etc/x-ui/x-ui.db"


def connect(db_path):
    if not os.path.exists(db_path):
        print(f"ERROR: database not found: {db_path}")
        sys.exit(1)
    return sqlite3.connect(db_path)


def load_inbounds(con):
    cur = con.execute("SELECT id, remark, port, stream_settings FROM inbounds")
    rows = cur.fetchall()
    return rows


def apply(db_path, dry_run):
    con = connect(db_path)
    rows = load_inbounds(con)

    changes = []  # [{id, remark, old_stream, new_stream}]

    for row_id, remark, port, stream_raw in rows:
        if not stream_raw:
            continue
        try:
            stream = json.loads(stream_raw)
        except json.JSONDecodeError:
            print(f"  [SKIP] id={row_id} remark={remark!r}: invalid JSON in stream_settings")
            continue

        proxies = stream.get("externalProxy")
        if not isinstance(proxies, list) or len(proxies) == 0:
            continue  # no externalProxy → skip

        # Skip if this dest already exists
        if any(p.get("dest") == NEW_DEST for p in proxies):
            print(f"  [SKIP] id={row_id} remark={remark!r}: {NEW_DEST} already present")
            continue

        # Copy forceTls from the first existing entry for consistency
        force_tls = proxies[0].get("forceTls", "same")

        new_entry = {
            "dest":     NEW_DEST,
            "port":     port,
            "forceTls": force_tls,
            "remark":   "",
        }

        new_proxies = proxies + [new_entry]
        new_stream = dict(stream)
        new_stream["externalProxy"] = new_proxies
        new_stream_raw = json.dumps(new_stream, ensure_ascii=False, separators=(",", ":"))

        changes.append({
            "id":         row_id,
            "remark":     remark,
            "old_stream": stream_raw,
            "new_stream": new_stream_raw,
        })
        print(f"  [{'DRY' if dry_run else 'ADD'}] id={row_id} remark={remark!r} port={port} "
              f"forceTls={force_tls!r} → adding {NEW_DEST}:{port}")

    if not changes:
        print("Nothing to do.")
        con.close()
        return

    if dry_run:
        print(f"\nDry run: {len(changes)} inbound(s) would be modified. No changes written.")
        con.close()
        return

    # Save backup before writing
    backup = {
        "timestamp": datetime.now().isoformat(),
        "db_path":   db_path,
        "changes":   [{"id": c["id"], "remark": c["remark"], "old_stream": c["old_stream"]}
                      for c in changes],
    }
    with open(BACKUP_FILE, "w", encoding="utf-8") as f:
        json.dump(backup, f, ensure_ascii=False, indent=2)
    print(f"\nBackup saved to {BACKUP_FILE}")

    # Apply
    cur = con.cursor()
    for c in changes:
        cur.execute("UPDATE inbounds SET stream_settings = ? WHERE id = ?",
                    (c["new_stream"], c["id"]))
    con.commit()
    con.close()
    print(f"Done: {len(changes)} inbound(s) updated.")


def rollback(db_path):
    if not os.path.exists(BACKUP_FILE):
        print(f"ERROR: backup file not found: {BACKUP_FILE}")
        sys.exit(1)

    with open(BACKUP_FILE, encoding="utf-8") as f:
        backup = json.load(f)

    print(f"Backup from: {backup['timestamp']}  db: {backup['db_path']}")
    con = connect(db_path)
    cur = con.cursor()
    for c in backup["changes"]:
        cur.execute("UPDATE inbounds SET stream_settings = ? WHERE id = ?",
                    (c["old_stream"], c["id"]))
        print(f"  [RESTORE] id={c['id']} remark={c['remark']!r}")
    con.commit()
    con.close()
    print(f"Rollback done: {len(backup['changes'])} inbound(s) restored.")

    os.rename(BACKUP_FILE, BACKUP_FILE + ".used")
    print(f"Backup renamed to {BACKUP_FILE}.used")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--db",       default=DEFAULT_DB, help="path to x-ui.db")
    parser.add_argument("--dry-run",  action="store_true", help="preview without writing")
    parser.add_argument("--rollback", action="store_true", help="restore from backup")
    args = parser.parse_args()

    if args.rollback:
        rollback(args.db)
    else:
        apply(args.db, args.dry_run)


if __name__ == "__main__":
    main()
