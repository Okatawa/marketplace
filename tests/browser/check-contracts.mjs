import Parser from "@apidevtools/swagger-parser";
import fs from "node:fs";
import assert from "node:assert/strict";
const api = await Parser.validate("api/openapi.yaml");
const routes = fs
  .readFileSync("services/gateway/router.go", "utf8")
  .matchAll(/"(GET|POST|PATCH|DELETE) (\/api\/v1\/[^"]+)"/g);
for (const [, method, path] of routes)
  assert(
    api.paths[path]?.[method.toLowerCase()],
    `Undocumented route: ${method} ${path}`,
  );
for (const name of ["envelope", "listing", "message"]) {
  const schema = JSON.parse(
    fs.readFileSync(`api/events/${name}.schema.json`, "utf8"),
  );
  assert(schema.$schema);
}
const msg = JSON.parse(
  fs.readFileSync("api/events/message.schema.json", "utf8"),
);
const fields = Object.keys(msg.allOf[1].properties.payload.properties);
assert.deepEqual(fields.sort(), [
  "conversation_id",
  "message_id",
  "recipient_id",
  "sender_id",
]);
console.log("OpenAPI and event contract structure verified");
