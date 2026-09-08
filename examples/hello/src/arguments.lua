local io = require("io")

local function frame(value: string): string
    return tostring(#value) .. ":" .. value
end

local function main(a: string, b: string, c: string, d: string, e: string, f: string): string
    local result = frame(a) .. frame(b) .. frame(c) .. frame(d) .. frame(e) .. frame(f)
    io.write(result .. "\n")
    return result
end

return { main = main }
