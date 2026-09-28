"""Source-side read-only aggregate export for an isolated preview, not a production API."""
import json
import sqlite3
import sys
import time
from datetime import date, datetime, timedelta, timezone
from zoneinfo import ZoneInfo


def export(path, from_text, to_text):
    zone = ZoneInfo("Asia/Shanghai")
    start, end = date.fromisoformat(from_text), date.fromisoformat(to_text)
    if start.isoformat() != from_text or end.isoformat() != to_text or not 0 <= (end-start).days <= 365:
        raise ValueError("Invalid date range")
    low = datetime.combine(start, datetime.min.time(), zone).astimezone(timezone.utc).strftime("%Y-%m-%d %H:%M:%S")
    high = datetime.combine(end+timedelta(days=1), datetime.min.time(), zone).astimezone(timezone.utc).strftime("%Y-%m-%d %H:%M:%S")
    types = ("canvas_image", "canvas_video", "image_generate", "video_generate")
    db = sqlite3.connect("file:"+path+"?mode=ro", uri=True, timeout=3)
    deadline = time.monotonic()+8
    db.set_progress_handler(lambda: int(time.monotonic()>deadline), 10000)
    try:
        db.execute("PRAGMA query_only=ON")
        db.execute("BEGIN")
        where = "created_at >= ? AND created_at < ? AND type IN (?,?,?,?)"
        args = (low, high, *types)
        total, success, failed, creators = db.execute("SELECT count(*),coalesce(sum(status='succeeded'),0),coalesce(sum(status='failed'),0),count(distinct user_id) FROM tasks WHERE "+where, args).fetchone()
        daily = db.execute("SELECT strftime('%Y-%m-%d',created_at,'+8 hours'),count(*),sum(status='succeeded'),sum(status='failed'),count(distinct user_id) FROM tasks WHERE "+where+" GROUP BY 1 ORDER BY 1",args).fetchall()
        groups = db.execute("SELECT type,count(*),sum(status='succeeded') FROM tasks WHERE "+where+" GROUP BY type ORDER BY count(*) DESC",args).fetchall()
        result = {
            "from":from_text,"to":to_text,"timezone":str(zone),"generatedAt":datetime.now(timezone.utc).isoformat(),
            "dataOrigin":"source_readonly_snapshot","previewTransaction":{"readOnly":bool(db.execute("PRAGMA query_only").fetchone()[0]),"isolation":"sqlite read transaction"},"sourceSchema":db.execute("SELECT max(version) FROM schema_migrations").fetchone()[0],
            "generationTasks":total,"successfulTasks":success,"failedTasks":failed,"activeCreators":creators,
            "taskTrend":[dict(day=r[0],count=r[1],succeeded=r[2],failed=r[3],creators=r[4]) for r in daily],
            "taskModels":[dict(model=r[0],count=r[1],succeeded=r[2]) for r in groups],
            "taskTypes":list(types),"activeUsers":None,"activeUsersBasis":"not_requested",
            "imageOutputs":None,"videoOutputs":None,"trackingStartedAt":None,"coverageComplete":False,"trend":[],"models":[],
            "warnings":["当前线上尚无生成产物事实表；任务数不是生成图片/视频件数。","活跃创作者是区间内创建上述生成任务的去重用户。","此数据为源端只读预览快照，正式统计API尚未发布。"],
        }
        return result
    finally:
        db.rollback()
        db.close()


if __name__ == "__main__":
    try:
        print(json.dumps({"code":0,"data":export(sys.argv[1],sys.argv[2],sys.argv[3])},ensure_ascii=False))
    except Exception as exc:
        print(json.dumps({"code":1,"errorType":type(exc).__name__}))
        raise SystemExit(1)
