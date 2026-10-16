// 视频模型的参考资产分类上限（图片 / 视频 / 音频）。
// 单一真源是后端 /video-presets 的 max_videos / max_audios（渠道导入时从上游
// 模型规格带入）；预设没声明的回落到模型行自己的 max_reference_videos/audios。
function declared(v) {
  return v === undefined || v === null ? null : Number(v)
}

// mediaCaps 返回 { images, videos, audios }（0 = 不支持该类），不支持图片以外的
// 参考资产时返回 null。model 兼容 /managed-models(type) 与 /models(kind) 两种字段。
export function mediaCaps(model, preset) {
  if ((model?.type || model?.kind) !== 'video') return null
  const videos = declared(preset?.max_videos) ?? declared(model?.max_reference_videos) ?? 0
  const audios = declared(preset?.max_audios) ?? declared(model?.max_reference_audios) ?? 0
  if (!videos && !audios) return null
  return { images: declared(preset?.max_images) ?? declared(model?.max_reference_images) ?? 0, videos, audios }
}

// presetMap 把 /video-presets 的列表转成 key → preset，便于按模型 id 取预设。
export function presetMap(list) {
  const out = {}
  for (const p of list || []) if (p?.key) out[p.key] = p
  return out
}
