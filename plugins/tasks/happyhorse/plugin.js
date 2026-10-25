// Local provider: preserve eflow Happyhorse's request and pricing inputs.
export const meta = {
  apiVersion: 1, key: "happyhorse", name: "Happyhorse", version: "1.0.0",
  author: { name: "eflow" }, channelTypes: [1001], fetchMode: "per_task",
  description: { en: "Local Happyhorse video generation", zh: "本地 Happyhorse 视频生成" },
  models: ["happyhorse-1.0-t2v", "happyhorse-1.0-i2v", "happyhorse-1.0-r2v", "happyhorse-1.0-video-edit"],
  usageSchema: {
    seconds: { type: "number", unit: "second", description: { en: "Video duration", zh: "生成视频时长" } },
    resolution: { enum: ["480P", "720P", "1080P"], description: { en: "Output resolution", zh: "输出视频分辨率" } },
  },
};

function convert(ctx) {
  const req = ctx.requestBody;
  if (!req || typeof req !== "object" || Array.isArray(req)) throw new Error("request must be an object");
  const model = ctx.upstreamModel || req.model;
  if (!meta.models.includes(model)) throw new Error("unsupported happyhorse model");
  if (typeof req.prompt !== "string" || !req.prompt.trim()) throw new Error("prompt is required");
  const metadata = req.metadata == null ? {} : req.metadata;
  if (typeof metadata !== "object" || Array.isArray(metadata)) throw new Error("invalid metadata");
  let resolution = "720P", ratio = "16:9";
  const dimensions = String(req.size || "").trim().toLowerCase().split(/[x*]/);
  if (dimensions.length === 2) {
    const w = Number(dimensions[0]), h = Number(dimensions[1]);
    if (Number.isInteger(w) && Number.isInteger(h) && w > 0 && h > 0) {
      const longest = Math.max(w, h);
      resolution = longest <= 832 ? "480P" : longest <= 1280 ? "720P" : "1080P";
      if (w === h) ratio = "1:1";
      else if (w * 9 === h * 16) ratio = "16:9";
      else if (w * 16 === h * 9) ratio = "9:16";
      else if (w * 3 === h * 4) ratio = "4:3";
      else if (w * 4 === h * 3) ratio = "3:4";
    }
  }
  if (metadata.resolution) resolution = metadata.resolution;
  if (metadata.ratio) ratio = metadata.ratio;
  if (!["480P", "720P", "1080P"].includes(resolution)) throw new Error("invalid resolution");
  if (!["16:9", "9:16", "1:1", "4:3", "3:4"].includes(ratio)) throw new Error("invalid ratio");
  const duration = Number(req.duration ?? req.seconds ?? 5);
  if (!Number.isInteger(duration) || duration < 3 || duration > 15) throw new Error("duration must be between 3 and 15 seconds");
  const input = { prompt: req.prompt };
  let action = "text_to_video";
  if (model.endsWith("-i2v") || model.endsWith("-r2v")) {
    if (typeof req.input_reference !== "string" || !req.input_reference.trim()) throw new Error("input_reference is required");
    input.media = [{ type: model.endsWith("-i2v") ? "first_frame" : "reference_image", url: req.input_reference }];
    action = "image_to_video";
  } else if (model.endsWith("-video-edit")) {
    if (typeof metadata.media_url !== "string" || !metadata.media_url.trim()) throw new Error("metadata.media_url is required");
    input.media = [{ type: "video", url: metadata.media_url }];
    action = "video_edit";
  }
  return { body: { model, input, parameters: { resolution, ratio, duration } }, action };
}

export function buildSubmitRequest(ctx) {
  const converted = convert(ctx);
  return { url: ctx.baseUrl + "/api/v1/services/aigc/video-generation/video-synthesis", method: "POST",
    headers: { Authorization: "Bearer " + String(ctx.apiKey || "").trim(), "Content-Type": "application/json", "X-DashScope-Async": "enable" },
    body: converted.body, action: converted.action };
}
export function parseSubmitResponse(ctx, response) {
  const body = response.body || {};
  if (body.code) throw new Error("happyhorse submission failed");
  if (!(body.output || {}).task_id) throw new Error("task_id is empty");
  return { taskId: body.output.task_id };
}
export function extractUsage(ctx) {
  const params = convert(ctx).body.parameters;
  return { seconds: params.duration, resolution: params.resolution };
}
// The existing Happyhorse contract bills the reserved duration; completion
// usage is intentionally empty until actual-duration settlement is approved.
export function extractUsageOnComplete() { return {}; }
export function buildQueryRequest(ctx) {
  return { url: ctx.baseUrl + "/api/v1/tasks/" + encodeURIComponent(ctx.taskId), method: "GET",
    headers: { Authorization: "Bearer " + String(ctx.apiKey || "").trim() } };
}
export function parseTaskResult(ctx, body) {
  const out = body.output || {};
  switch (out.task_status) {
    case "PENDING": return { status: "QUEUED" };
    case "RUNNING": return { status: "IN_PROGRESS" };
    case "SUCCEEDED": return { status: "SUCCESS", url: out.video_url || "" };
    case "FAILED": case "CANCELED": case "UNKNOWN":
      return { status: "FAILURE", reason: body.message || (out.message ? "task failed, code: " + (out.code || "") + " , message: " + out.message : "task failed") };
    default: return { status: "QUEUED" };
  }
}

export function buildContentRequest(ctx) {
  const url = ((ctx.data || {}).output || {}).video_url;
  if (!url) throw new Error("artifact_not_found");
  return { url, method: "GET", credentialless: true };
}

export function listArtifacts(task) {
  return task.status === "SUCCESS" && ((task.data || {}).output || {}).video_url
    ? [{ key: "video", type: "video", mimeType: "video/mp4" }] : [];
}
