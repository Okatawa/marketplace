package listings

import "embed"

//go:embed migrations/*.sql
var Migrations embed.FS
