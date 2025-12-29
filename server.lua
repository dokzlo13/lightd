-- server.lua
-- HTTP Webhook-triggered actions using the new chainable Hue API

local webhook = require("events.webhook")
local action = require("action")
local hue = require("hue")
local log = require("log")

--------------------------------------------------------------------------------
-- Webhook Endpoints
--------------------------------------------------------------------------------

-- POST /light/toggle - Toggle a light by ID from JSON body
-- Body: {"id": "1"}
webhook.define("POST", "/light/toggle", "toggle_light")

-- POST /light/set - Set light state from JSON body
-- Body: {"id": "1", "on": true, "bri": 200, "hue": 40000}
webhook.define("POST", "/light/set", "set_light_state")

-- POST /group/scene - Set scene for a group
-- Body: {"id": "2", "scene": "Relax"}
webhook.define("POST", "/group/scene", "set_group_scene")

-- POST /group/toggle - Toggle a group on/off
-- Body: {"id": "2"}
webhook.define("POST", "/group/toggle", "toggle_group_http")

-- POST /group/{id}/toggle - Toggle a group using path parameter
-- Example: POST /group/2/toggle
webhook.define("POST", "/group/{id}/toggle", "toggle_group_by_path")

-- POST /all/off - Turn off all groups
webhook.define("POST", "/all/off", "all_lights_off", { groups = {"2", "81", "82"} })

--------------------------------------------------------------------------------
-- Actions
--------------------------------------------------------------------------------

-- Toggle a specific light by ID
action.define("toggle_light", function(ctx, args)
    if not ctx.request or not ctx.request.json then
        log.error("toggle_light requires JSON body with 'id' field")
        return
    end
    
    local light_id = ctx.request.json.id
    if not light_id then
        log.error("Missing 'id' in request body")
        return
    end
    
    log.info("Toggling light: " .. tostring(light_id))
    
    local light, err = hue.light(light_id)
    if err then
        log.error("Failed to get light " .. tostring(light_id) .. ": " .. err)
        return
    end
    
    -- Simple toggle using chainable API
    light:toggle()
    log.info("Light '" .. light:name() .. "' toggled")
end)

-- Set light state from JSON body
-- Example: POST /light/set
-- Body: {"id": "1", "on": true, "bri": 200, "hue": 40000}
action.define("set_light_state", function(ctx, args)
    if not ctx.request or not ctx.request.json then
        log.error("set_light_state requires JSON body")
        return
    end
    
    local json = ctx.request.json
    local light_id = json.id
    
    if not light_id then
        log.error("Missing 'id' in request body")
        return
    end
    
    log.info("Setting state for light: " .. tostring(light_id))
    
    local light, err = hue.light(light_id)
    if err then
        log.error("Failed to get light " .. tostring(light_id) .. ": " .. err)
        return
    end
    
    -- Use the generic set_state for maximum flexibility
    -- Accepts: on, bri, hue, sat, ct, xy, transitiontime, alert, effect
    light:set_state(json)
    log.info("Light '" .. light:name() .. "' state updated")
end)

-- Set scene for a group
-- Example: POST /group/scene
-- Body: {"id": "2", "scene": "Relax"}
action.define("set_group_scene", function(ctx, args)
    if not ctx.request or not ctx.request.json then
        log.error("set_group_scene requires JSON body")
        return
    end
    
    local json = ctx.request.json
    local group_id = json.id
    local scene_name = json.scene
    
    if not group_id then
        log.error("Missing 'id' in request body")
        return
    end
    
    if not scene_name then
        log.error("Missing 'scene' in request body")
        return
    end
    
    log.info("Setting scene '" .. scene_name .. "' for group " .. tostring(group_id))
    
    local group, err = hue.group(group_id)
    if err then
        log.error("Failed to get group " .. tostring(group_id) .. ": " .. err)
        return
    end
    
    -- Chainable scene activation
    group:set_scene(scene_name)
    log.info("Scene '" .. scene_name .. "' activated on group '" .. group:name() .. "'")
end)

-- Toggle a group on/off via HTTP
action.define("toggle_group_http", function(ctx, args)
    if not ctx.request or not ctx.request.json then
        log.error("toggle_group_http requires JSON body with 'id' field")
        return
    end
    
    local group_id = ctx.request.json.id
    if not group_id then
        log.error("Missing 'id' in request body")
        return
    end
    
    log.info("Toggling group: " .. tostring(group_id))
    
    local group, err = hue.group(group_id)
    if err then
        log.error("Failed to get group " .. tostring(group_id) .. ": " .. err)
        return
    end
    
    -- Toggle and log result
    group:toggle()
    log.info("Group '" .. group:name() .. "' toggled")
end)

-- Toggle a group using path parameter (e.g., POST /group/2/toggle)
action.define("toggle_group_by_path", function(ctx, args)
    if not ctx.request then
        log.error("toggle_group_by_path requires request context")
        return
    end
    
    -- Get group ID from path parameter
    local group_id = ctx.request.path_params.id
    if not group_id then
        log.error("Missing 'id' path parameter")
        return
    end
    
    log.info("Toggling group from path: " .. tostring(group_id))
    
    local group, err = hue.group(group_id)
    if err then
        log.error("Failed to get group " .. tostring(group_id) .. ": " .. err)
        return
    end
    
    -- Toggle and log result
    group:toggle()
    log.info("Group '" .. group:name() .. "' toggled via path param")
end)

-- Turn off all lights in specified groups
action.define("all_lights_off", function(ctx, args)
    local group_ids = args.groups or {}
    log.info("Turning off " .. #group_ids .. " groups")
    
    for _, group_id in ipairs(group_ids) do
        local group, err = hue.group(group_id)
        if err then
            log.warn("Failed to get group " .. group_id .. ": " .. err)
        else
            group:off()
            log.info("Group '" .. group:name() .. "' turned off")
        end
    end
end)

--------------------------------------------------------------------------------
-- Example: Using chaining for complex operations
--------------------------------------------------------------------------------

-- POST /demo/colorful - Set all lights to different colors
webhook.define("POST", "/demo/colorful", "colorful_mode")

action.define("colorful_mode", function(ctx, args)
    log.info("Colorful mode activated!")
    
    -- Get all lights and make them colorful
    local lights, err = hue.lights()
    if err then
        log.error("Failed to get lights: " .. err)
        return
    end
    
    -- Chain multiple operations on each light
    for i, light in ipairs(lights) do
        -- Rotate through colors using hue values (0-65535)
        local hue_value = (i * 10000) % 65535
        
        -- Chainable: turn on, set brightness, set color
        light:on():set_bri(254):set_hue(hue_value):set_sat(254)
        log.debug("Light '" .. light:name() .. "' set to hue " .. hue_value)
    end
    
    log.info("All lights set to colorful mode!")
end)

-- POST /demo/warm - Set all lights to warm white
webhook.define("POST", "/demo/warm", "warm_mode")

action.define("warm_mode", function(ctx, args)
    log.info("Warm mode activated!")
    
    local lights, err = hue.lights()
    if err then
        log.error("Failed to get lights: " .. err)
        return
    end
    
    for _, light in ipairs(lights) do
        -- Use set_state for multiple properties at once
        light:set_state({
            on = true,
            bri = 200,
            ct = 400  -- warm white (mirek: 153=cool, 500=warm)
        })
    end
    
    log.info("All lights set to warm white!")
end)

log.info("=== Server webhooks loaded ===")
