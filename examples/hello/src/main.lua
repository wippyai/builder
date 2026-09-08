local io = require("io")

local function main(name: string?): string
    local greeting = "Hello, " .. (name or "world") .. "!"
    io.write(greeting .. "\n")
    return greeting
end

return { main = main }
