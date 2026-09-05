# {{NAME}}

## 接口

该协议对应影策现有的 `comfy-adapter` 外部适配服务，不直接把浏览器请求发送到 ComfyUI。创建任务使用 `POST /v1/jobs`，查询任务使用 `GET /v1/jobs/{job_id}`。适配服务把版本化工作流模板、用户参数和参考图片编译成 ComfyUI prompt，随后调用目标 ComfyUI 的 `/prompt`、`/history/{prompt_id}`、`/view` 与 `/upload/image` 接口。影策后端保存渠道地址和适配器令牌，任务工作器负责创建、轮询、下载结果以及写入资源存储。

{{OPERATIONS}}

`GET /health` 用于独立检查适配服务、已配置 Provider 数量和工作流注册数量；`GET /v1/providers`、`GET /v1/workflows` 与 `GET /v1/models` 用于查看可用目标和工作流目录。健康检查成功只代表入口可达，实际生成仍要求工作流存在、ComfyUI Provider 可达、节点与模型齐全，并且输出节点能返回图片、视频或音频文件。

## 模型

渠道模型的上游模型标识对应 `workflow_key`，例如文生图、图生图、文生视频和图生视频可以分别指向不同的注册工作流。一个工作流由注册表中的 `key`、`revision`、能力、源 JSON 与字段映射组成；适配器按 revision 读取固定版本，不能在任务运行期间静默切换模板。工作流更新后应先在测试渠道验证节点、模型文件、输入映射和输出类型，再修改生产模型所引用的 key 或 revision。

同一个 `comfyui-workflow` 协议同时支持图片和视频能力。能力由渠道模型和具体工作流清单共同约束，不能仅根据模型名称猜测。图片模型应只返回图片资源，视频模型应返回视频资源；工作流同时产生音频时，适配器会保留音频输出，但画布是否消费该资源仍由调用任务决定。Provider 由 `provider_id` 选择，默认值是 `default`，实际地址来自适配服务的 `COMFY_PROVIDERS_JSON` 或 `COMFY_URL`，不写入渠道模型正文。配置多个 Provider 并启用 `COMFY_AUTO_BALANCE` 时，`default` 请求会选择当前可达且 ComfyUI 队列最短的 Provider；显式指定其他 Provider ID 时仍固定路由到该节点。

## 参数

创建请求为 JSON。稳定字段包括 `workflow_key`、`provider_id`、`prompt`、`negative_prompt`、`input_images`、`width`、`height`、`duration`、`generate_audio`、`seed`、`batch_size`、`denoise` 与 `metadata`。工作流编译器只修改注册表声明的节点和字段，未声明字段不会凭名称注入任意节点。图片列表最多九张；单个下载或 data URL 解码后的文件最多 30 MB；批次数量范围为一到四；宽高范围为 64 到 8192；时长大于零且最多 60 秒；降噪范围为零到一。

`input_images` 可以使用 HTTP(S) URL、带 MIME 的 data URL 或 `comfy://` 已存在文件引用。HTTP(S) 下载默认拒绝私网、链路本地和重定向地址，只有显式列入 `COMFY_INPUT_HOST_ALLOWLIST` 的主机例外。外部图片会先上传到选定 ComfyUI 的 `/upload/image`，然后把服务端文件名写入工作流。生产环境的素材应优先使用平台资源存储的稳定 URL，避免临时签名地址在排队期间过期。

{{PARAMETERS}}

## 鉴权与状态

适配服务令牌通过 `Authorization: Bearer <token>` 发送；令牌只保存在后端渠道密钥中。ComfyUI Provider 自身若需要令牌，由适配服务读取 Provider 配置并在服务间请求时添加。创建成功返回编码后的 `id`、原始 `promptId`、`providerId`、`workflowKey`、`workflowRevision` 和 `submitted` 状态。轮询会读取 ComfyUI history：尚未完成时返回处理中，执行错误时返回失败，找到输出文件时返回成功和资源列表。

任务 ID 同时绑定 Provider 和 prompt ID，防止轮询时误查另一台 ComfyUI。输出下载由适配服务按任务和索引读取，文件名、子目录和类型来自 history 结果。若 ComfyUI 创建接口没有返回 `prompt_id`、history 报错、成功记录没有可识别输出，或输出下载失败，适配器必须返回真实错误，不能把空结果标记为完成。

## 官方

- [ComfyUI Server Routes](https://github.com/comfyanonymous/ComfyUI/blob/master/server.py)
- [ComfyUI Examples](https://comfyanonymous.github.io/ComfyUI_examples/)
- [ComfyUI GitHub](https://github.com/comfyanonymous/ComfyUI)

本文的 `/v1/jobs` 合同属于影策 `comfy-adapter`，不是 ComfyUI 官方接口。对接时应分别验证适配服务健康、工作流目录、ComfyUI 队列和一次真实媒体输出，不能用任一单独的 HTTP 200 代替端到端验证。

{{CONTRACT}}
