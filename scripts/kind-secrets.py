"""Create ignored env files for local kind. Never print credentials."""
import os
from pathlib import Path
from urllib.parse import quote

env = dict(line.split("=", 1) for line in Path(".local/.env").read_text().splitlines() if "=" in line)
root = Path(".local/kind-secrets")
root.mkdir(mode=0o700, parents=True, exist_ok=True)

def write(name, data):
    path = root / (name + ".env")
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(fd, "w") as f:
        f.write("".join(f"{k}={v}\n" for k, v in data.items()))

write("marketplace-infra", {k: env[k] for k in ["POSTGRES_PASSWORD", "APP_DB_PASSWORD", "S3_ACCESS_KEY", "S3_SECRET_KEY"]})
for service in ["gateway", "accounts", "listings", "search", "messaging", "notifications"]:
    data = {k: env[k] for k in ["JWT_PUBLIC_KEY", "SERVICE_TOKEN"]}
    if service != "gateway":
        data["DATABASE_URL"] = f"postgres://{service}:{quote(env['APP_DB_PASSWORD'], safe='')}@postgres:5432/{service}?sslmode=disable"
    if service == "accounts":
        data["JWT_PRIVATE_KEY"] = env["JWT_PRIVATE_KEY"]
    if service == "listings":
        data.update({k: env[k] for k in ["S3_ACCESS_KEY", "S3_SECRET_KEY"]})
    write("marketplace-" + service, data)
