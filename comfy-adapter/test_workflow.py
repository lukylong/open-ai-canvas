import unittest

from workflow import compile_workflow, load_registry, public_model


class WorkflowCompilerTests(unittest.TestCase):
    def test_registry_contains_inherited_enabled_workflows(self):
        registry = load_registry()
        self.assertEqual(len(registry), 16)
        self.assertIn("qwen_image_2_1", registry)
        self.assertIn("qwen_image", registry)
        self.assertIn("minimax_h3_r2v", registry)
        self.assertIn("minimax_h3_hybrid_v0331_t2v", registry)
        self.assertIn("minimax_h3_hybrid_v0331_i2v", registry)
        self.assertEqual(
            registry["minimax_h3_hybrid_v0331_t2v"].revision,
            registry["minimax_h3_hybrid_v0331_i2v"].revision,
        )
        self.assertTrue(all(item.revision for item in registry.values()))

    def test_every_enabled_workflow_compiles_with_its_required_inputs(self):
        for spec in load_registry().values():
            with self.subTest(workflow=spec.key):
                images = ["reference-1.png", "reference-2.png"] if spec.raw.get("load_image_node") or spec.raw.get("reference_node") else []
                result = compile_workflow(spec, {"prompt": "人物向前一步", "negative_prompt": "watermark", "width": 1024, "height": 576, "duration": 5, "batch_size": 2}, images, "all-workflows")
                self.assertTrue(result)

    def test_model_catalog_exposes_every_workflow_with_routing_metadata(self):
        models = {item.key: public_model(item) for item in load_registry().values()}
        self.assertEqual(len(models), 16)
        self.assertEqual(models["qwen_image"]["model_type"], "image")
        self.assertEqual(models["qwen_image"]["supported_endpoint_types"], ["comfyui-workflow", "text_to_image"])
        self.assertEqual(models["qwen_image"]["max_images"], 0)
        self.assertEqual(models["qwen_image_2_1"]["supported_endpoint_types"], ["comfyui-workflow", "text_to_image"])
        self.assertEqual(models["qwen_image_2_1"]["max_images"], 0)
        self.assertEqual(models["qwen_image_edit_2509"]["min_images"], 1)
        self.assertEqual(models["minimax_h3_t2v_official"]["supported_endpoint_types"], ["comfyui-workflow", "text_to_video"])
        self.assertEqual(models["minimax_h3_r2v_4step"]["max_images"], 9)
        self.assertIn("reference_to_video", models["minimax_h3_r2v_4step"]["supported_endpoint_types"])
        self.assertEqual(models["minimax_h3_hybrid_v0331_t2v"]["min_images"], 0)
        self.assertEqual(models["minimax_h3_hybrid_v0331_i2v"]["max_images"], 9)

    def test_qwen21_uses_validated_native_nodes_and_blackwell_attention(self):
        spec = load_registry()["qwen_image_2_1"]
        graph = compile_workflow(spec, {"prompt": "春日咖啡", "negative_prompt": "watermark", "width": 1920, "height": 1080, "seed": 17, "batch_size": 2}, [], "qwen21-test")
        self.assertEqual(graph["452"]["inputs"]["prompt"], "春日咖啡")
        self.assertEqual(graph["452"]["inputs"]["negative_prompt"], "watermark")
        self.assertEqual(graph["458"]["inputs"]["model"], ["476", 0])
        self.assertEqual(graph["476"]["inputs"]["attention"], "comfy kitchen attention")
        self.assertEqual(graph["458"]["inputs"]["cfg"], 1)
        self.assertEqual(graph["458"]["inputs"]["steps"], 25)
        self.assertEqual(graph["458"]["inputs"]["seed"], 17)
        size = graph["456"]["inputs"]
        self.assertEqual(size["width"] % 32, 0)
        self.assertEqual(size["height"] % 32, 0)
        self.assertLessEqual(size["width"] * size["height"], 1024 * 1024)
        self.assertEqual(size["batch_size"], 2)
        self.assertEqual(graph["461"]["inputs"]["filename_prefix"], "QwenImage21_qwen21-test")
        pristine = compile_workflow(spec, {"prompt": "second"}, [], "second")
        self.assertEqual(pristine["456"]["inputs"]["width"], 1024)
        self.assertEqual(pristine["456"]["inputs"]["batch_size"], 1)

    def test_text_to_image_parameters_are_patched_without_mutating_source(self):
        spec = load_registry()["qwen_image"]
        first = compile_workflow(spec, {"prompt": "first", "width": 1024, "height": 768, "seed": 42, "batch_size": 2}, [], "job-1")
        second = compile_workflow(spec, {"prompt": "second", "width": 512, "height": 512, "seed": 7, "batch_size": 1}, [], "job-2")
        self.assertEqual(first[spec.raw["prompt_node"]]["inputs"][spec.raw["prompt_input"]], "first")
        self.assertEqual(second[spec.raw["prompt_node"]]["inputs"][spec.raw["prompt_input"]], "second")
        self.assertEqual(first[spec.raw["batch_node"]]["inputs"][spec.raw["batch_input"]], 2)

    def test_reference_video_workflow_injects_all_reference_images(self):
        spec = load_registry()["minimax_h3_r2v"]
        workflow = compile_workflow(spec, {"prompt": "move", "duration": 5, "width": 768, "height": 1344, "seed": 9}, ["one.png", "two.png"], "job-r2v")
        inputs = workflow[spec.raw["reference_node"]]["inputs"]
        self.assertIn(f'{spec.raw["reference_input_prefix"]}0', inputs)
        self.assertIn(f'{spec.raw["reference_input_prefix"]}1', inputs)

    def test_hybrid_v0331_switch_uses_one_file_for_text_and_image_modes(self):
        registry = load_registry()
        text_spec = registry["minimax_h3_hybrid_v0331_t2v"]
        image_spec = registry["minimax_h3_hybrid_v0331_i2v"]
        self.assertEqual(text_spec.file, image_spec.file)

        text_workflow = compile_workflow(
            text_spec,
            {"prompt": "A lighthouse shines through rain.", "duration": 3, "width": 1280, "height": 720, "seed": 11},
            [],
            "hybrid-t2v",
        )
        self.assertNotIn("ref_images.ref_image_0", text_workflow["213"]["inputs"])
        self.assertTrue(text_workflow["213"]["inputs"]["prompt"].startswith("A lighthouse shines through rain."))
        self.assertIn("No character speaks", text_workflow["213"]["inputs"]["prompt"])
        self.assertLessEqual(text_workflow["213"]["inputs"]["width"] * text_workflow["213"]["inputs"]["height"], 0.5 * 1024 * 1024)
        self.assertEqual(text_workflow["415"]["inputs"]["mode.width"], 1280)
        self.assertEqual(text_workflow["415"]["inputs"]["mode.height"], 704)

        image_workflow = compile_workflow(
            image_spec,
            {"prompt": "The subject turns toward camera.", "duration": 9, "width": 720, "height": 1280, "seed": 12},
            ["subject.png"],
            "hybrid-i2v",
        )
        self.assertEqual(image_workflow["213"]["inputs"]["ref_images.ref_image_0"], ["canvas_reference_1", 0])
        self.assertTrue(image_workflow["415"]["inputs"]["enable_chunking"])

    def test_hybrid_v0331_rejects_more_than_nine_references(self):
        spec = load_registry()["minimax_h3_hybrid_v0331_i2v"]
        with self.assertRaisesRegex(ValueError, "at most 9"):
            compile_workflow(spec, {"prompt": "move"}, [f"{index}.png" for index in range(10)], "too-many")

    def test_hybrid_audio_without_dialogue_forbids_invented_speech(self):
        spec = load_registry()["minimax_h3_hybrid_v0331_t2v"]
        workflow = compile_workflow(
            spec,
            {"prompt": "一位老人站在货架前犹豫。", "generate_audio": True},
            [],
            "no-dialogue",
        )
        prompt = workflow["213"]["inputs"]["prompt"]
        self.assertIn("No character speaks", prompt)
        self.assertIn("invented language", prompt)
        self.assertIn("audio", workflow["322"]["inputs"])

    def test_hybrid_audio_tags_explicit_chinese_dialogue_exactly(self):
        spec = load_registry()["minimax_h3_hybrid_v0331_t2v"]
        workflow = compile_workflow(
            spec,
            {"prompt": "人物口播：“大家好，欢迎回来”", "generate_audio": True},
            [],
            "dialogue",
        )
        prompt = workflow["213"]["inputs"]["prompt"]
        self.assertIn("<d>[Chinese] 大家好，欢迎回来</d>", prompt)
        self.assertIn("Do not translate, paraphrase, replace, repeat, or invent any syllable", prompt)

    def test_hybrid_audio_can_be_disabled_without_changing_video_graph(self):
        spec = load_registry()["minimax_h3_hybrid_v0331_t2v"]
        workflow = compile_workflow(
            spec,
            {"prompt": "A silent landscape.", "generate_audio": False},
            [],
            "silent",
        )
        self.assertNotIn("audio", workflow["322"]["inputs"])
        self.assertEqual(workflow["322"]["inputs"]["images"], ["330", 0])

    def test_hybrid_audio_rejects_chinese_dialogue_that_cannot_fit_duration(self):
        spec = load_registry()["minimax_h3_hybrid_v0331_t2v"]
        with self.assertRaisesRegex(ValueError, "too long for 1 seconds"):
            compile_workflow(
                spec,
                {"prompt": "人物口播：“欢迎大家来到今天的节目现场”", "duration": 1, "generate_audio": True},
                [],
                "dialogue-too-long",
            )


if __name__ == "__main__":
    unittest.main()
