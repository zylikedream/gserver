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

-- notice: redis6.2以后 function只能使用local 定义，不能使用function xx() 这样会报错Attempt to modify a readonly table https://blog.csdn.net/jj1245_/article/details/149157715
local unregister_actor_node = function(id, node)
    local v = redis.call("GET", id)
    if v == node then
        return redis.call("DEL", id)
    else
        return 0
    end
end

if funcName == 'unregister_actor_node' then
    return unregister_actor_node(args["id"], args["node"])
end
return 0
