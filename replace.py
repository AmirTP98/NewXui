#!/usr/bin/env python3
"""
Replace an externalProxy dest domain across all inbounds:
  account.wordqress.store -> dash.wordqress.store

Only the "dest" field of externalProxy entries that match OLD_DEST is
changed; everything else (port, forceTls, remark, other settings) is
left untouched.

Usage:
  python3 replace_external_proxy_domain.py                        # apply
  python3 replace_external_proxy_domain.py --dry-run              # preview only
  python3 replace_external_proxy_domain.py --rollback             # undo last apply
  python3 replace_external_proxy_domain.py --db /path/to/x-ui.db  # custom db path
"""

import argparse
import json
import os
import sqlite3
import sys
from datetime import datetime

OLD_DEST = "account.wordqress.store"
NEW_DEST = "dash.wordqress.store"
BACKUP_FILE = "external_proxy_domain_backup.json"
DEFAULT_DB = "/etc/x-ui/x-ui.db"


def connect(db_path):
    if not os.path.exists(db_path):
        print(f"ERROR: database not found: {db_path}")
        sys.exit(1)
    return sqlite3.connect(db_path)


def load_inbounds(con):
    cur = con.execute("SELECT id, remark, stream_settings FROM inbounds")
    return cur.fetchall()


def apply(db_path, dry_run):
    con = connect(db_path)
    rows = load_inbounds(con)

    changes = []  # [{id, remark, old_stream, new_stream, count}]

    for row_id, remark, stream_raw in rows:
        if not stream_raw:
            continue
        try:
            stream = json.loads(stream_raw)
        except json.JSONDecodeError:
            print(f"  [SKIP] id={row_id} remark={remark!r}: invalid JSON in stream_settings")
            continue

        proxies = stream.get("externalProxy")
        if not isinstance(proxies, list) or len(proxies) == 0:
            continue  # no externalProxy -> skip

        count = 0
        new_proxies = []
        for p in proxies:
            if isinstance(p, dict) and p.get("dest") == OLD_DEST:
                p = dict(p)
                p["dest"] = NEW_DEST
                count += 1
            new_proxies.append(p)

        if count == 0:
            continue  # no match -> skip

        new_stream = dict(stream)
        new_stream["externalProxy"] = new_proxies
        new_stream_raw = json.dumps(new_stream, ensure_ascii=False, separators=(",", ":"))

        changes.append({
            "id": row_id,
            "remark": remark,
            "old_stream": stream_raw,
            "new_stream": new_stream_raw,
        })
        print(f"  [{'DRY' if dry_run else 'UPD'}] id={row_id} remark={remark!r}: "
              f"{count} entr{'y' if count == 1 else 'ies'} {OLD_DEST} -> {NEW_DEST}")

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
        "db_path": db_path,
        "changes": [{"id": c["id"], "remark": c["remark"], "old_stream": c["old_stream"]}
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
    parser.add_argument("--db", default=DEFAULT_DB, help="path to x-ui.db")
    parser.add_argument("--dry-run", action="store_true", help="preview without writing")
    parser.add_argument("--rollback", action="store_true", help="restore from backup")
    args = parser.parse_args()

    if args.rollback:
        rollback(args.db)
    else:
        apply(args.db, args.dry_run)


if __name__ == "__main__":
    main()
