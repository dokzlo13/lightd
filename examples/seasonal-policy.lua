-- Pure calendar data: safe to load through --preview-policy or dofile().
-- Keep the six IDs stable so manual scene cycling retains its identity.
-- Earlier bedtime values have deliberately not been selected automatically.
return {
    id = "lighting",
    version = "winter-1",
    tag = "scene_set",
    phases = {
        { id = "scene:dawn", at = "@dawn", action = "store_scene_all", args = { scene = "Energize" } },
        { id = "scene:noon", at = "@noon + 30m", action = "store_scene_all", args = { scene = "Concentrate" } },
        { id = "scene:sunset-2h", relative_to = "scene:sunset", offset = "-1h", action = "store_scene_all", args = { scene = "Read" } },
        {
            id = "scene:sunset", at = "@sunset + 30m",
            offset_curve = {
                input = "day_length", from = "9h20m6s", to = "6h",
                start = "0m", finish = "1h", easing = "smoothstep",
            },
            earliest = "17:00", latest = "20:30",
            action = "store_scene_all", args = { scene = "Relax" },
        },
        { id = "scene:2245", at = "22:45", action = "store_scene_all", args = { scene = "Rest" } },
        { id = "scene:2359", at = "23:50", action = "store_scene_all", args = { scene = "Nightlight" } },
    },
}
