import sqlite3, glob, os

for p in glob.glob("**/*.db", recursive=True) + glob.glob("C:/NKB-PoC/**/*.db", recursive=True):
    print("DB found:", p)
    try:
        conn = sqlite3.connect(p)
        cur = conn.cursor()
        for tbl in cur.execute("SELECT name FROM sqlite_master WHERE type='table'").fetchall():
            t = tbl[0]
            print("  Table:", t)
            if "disk" in t.lower() or "client" in t.lower():
                rows = cur.execute(f"SELECT * FROM {t}").fetchall()
                print(f"    Rows ({len(rows)}):")
                for r in rows:
                    print("     ", r)
        conn.close()
    except Exception as e:
        print("  Error:", e)
