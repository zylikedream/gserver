local parse_args = function ()
    local args = {}
    for i = 1, #KEYS do
        local key = KEYS[i]
        local value = ARGV[i]
        args[key] = value
    end 
    return args
end

local args = parse_args()
local funcName = args["func"] or ''

function unregister_grain_node(id, node)
    local v = redis.call("GET", id)
    if v == node then
        return redis.call("DEL", id)
    else
        return 0
    end
end

if funcName == 'unregister_grain_node' then
    unregister_grain_node(args["id"], args["node"])
end