import fs from "node:fs";
import path from "node:path";

function readJson(filePath) {
  const raw = fs.readFileSync(filePath, "utf-8");
  return JSON.parse(raw);
}

function quoteKey(key) {
  if (/^[A-Za-z_$][A-Za-z0-9_$]*$/.test(key)) {
    return key;
  }
  return JSON.stringify(key);
}

function parseRef(ref) {
  const parts = ref.split("/");
  return parts[parts.length - 1];
}

function resolveSchema(schema, context) {
  if (!schema || Object.keys(schema).length === 0) {
    return "unknown";
  }

  if (schema.$ref) {
    return parseRef(schema.$ref);
  }

  if (schema.anyOf) {
    const members = schema.anyOf.map((item) => resolveSchema(item, context));
    return Array.from(new Set(members)).join(" | ");
  }

  if (schema.oneOf) {
    const members = schema.oneOf.map((item) => resolveSchema(item, context));
    return Array.from(new Set(members)).join(" | ");
  }

  if (schema.enum) {
    return schema.enum.map((item) => JSON.stringify(item)).join(" | ");
  }

  if (schema.type === "string") {
    return "string";
  }
  if (schema.type === "boolean") {
    return "boolean";
  }
  if (schema.type === "integer" || schema.type === "number") {
    return "number";
  }
  if (schema.type === "null") {
    return "null";
  }

  if (schema.type === "array") {
    const itemType = resolveSchema(schema.items ?? {}, context);
    return `Array<${itemType}>`;
  }

  if (schema.type === "object" || schema.properties || schema.additionalProperties) {
    if (schema.properties) {
      const required = new Set(schema.required ?? []);
      const lines = Object.entries(schema.properties).map(([prop, propSchema]) => {
        const optional = required.has(prop) ? "" : "?";
        return `  ${quoteKey(prop)}${optional}: ${resolveSchema(propSchema, context)};`;
      });
      return `{\n${lines.join("\n")}\n}`;
    }

    if (schema.additionalProperties) {
      if (schema.additionalProperties === true) {
        return "Record<string, unknown>";
      }
      return `Record<string, ${resolveSchema(schema.additionalProperties, context)}>`;
    }

    return "Record<string, unknown>";
  }

  return "unknown";
}

function buildSchemasBlock(openapi) {
  const schemas = openapi.components?.schemas ?? {};
  const blocks = [];

  for (const [name, schema] of Object.entries(schemas)) {
    const description = schema.description ? `/** ${schema.description} */\n` : "";
    const resolved = resolveSchema(schema, { openapi });
    if (
      schema.type === "object" ||
      schema.properties ||
      schema.additionalProperties
    ) {
      blocks.push(`${description}export interface ${name} ${resolved}`);
    } else {
      blocks.push(`${description}export type ${name} = ${resolved};`);
    }
  }

  return blocks.join("\n\n");
}

function pickResponseSchema(operation) {
  const responses = operation.responses ?? {};
  const candidates = Object.keys(responses).filter((code) => code.startsWith("2"));
  candidates.sort();
  const picked = candidates[0];
  if (!picked) {
    return "unknown";
  }

  const content = responses[picked]?.content?.["application/json"]?.schema;
  if (!content) {
    return "unknown";
  }
  return resolveSchema(content, {});
}

function pickRequestBodySchema(operation) {
  const schema = operation.requestBody?.content?.["application/json"]?.schema;
  if (!schema) {
    return "never";
  }
  return resolveSchema(schema, {});
}

function buildOperationsBlock(openapi) {
  const lines = [];
  const paths = openapi.paths ?? {};

  for (const [pathKey, methods] of Object.entries(paths)) {
    for (const [method, operation] of Object.entries(methods)) {
      const methodUpper = method.toUpperCase();
      const opId =
        operation.operationId || `${method}_${pathKey}`.replace(/[^A-Za-z0-9_]/g, "_");
      const requestType = pickRequestBodySchema(operation);
      const responseType = pickResponseSchema(operation);
      lines.push(
        `  ${JSON.stringify(opId)}: { method: ${JSON.stringify(methodUpper)}; path: ${JSON.stringify(
          pathKey,
        )}; requestBody: ${requestType}; response: ${responseType}; };`,
      );
    }
  }

  return `export interface ApiOperationMap {\n${lines.join("\n")}\n}`;
}

function buildFile(openapi, sourcePath) {
  const header = `/* eslint-disable */\n/**
 * 由脚本自动生成，请勿手改。
 * 来源: ${sourcePath}
 */`;

  const schemasBlock = buildSchemasBlock(openapi);
  const operationsBlock = buildOperationsBlock(openapi);

  return `${header}

// -----------------------------
// Components Schemas
// -----------------------------

${schemasBlock}

// -----------------------------
// Operations
// -----------------------------

${operationsBlock}
`;
}

function main() {
  const inputArg = process.argv[2] ?? "./openapi.json";
  const outputArg = process.argv[3] ?? "./src/api/types.ts";
  const cwd = process.cwd();
  const inputPath = path.resolve(cwd, inputArg);
  const outputPath = path.resolve(cwd, outputArg);

  const openapi = readJson(inputPath);
  const generated = buildFile(openapi, inputArg);

  fs.mkdirSync(path.dirname(outputPath), { recursive: true });
  fs.writeFileSync(outputPath, generated, "utf-8");
  // eslint-disable-next-line no-console
  console.log(`[openapi-types] generated: ${outputPath}`);
}

main();

