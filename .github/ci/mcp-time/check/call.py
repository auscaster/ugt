"""The task, as an AI app does it: call the time server's get_current_time
tool over MCP, for the zone in inputs/timezone.txt."""
import asyncio

from mcp import ClientSession, StdioServerParameters
from mcp.client.stdio import stdio_client


async def main():
    zone = open("/work/inputs/timezone.txt").read().strip()
    async with stdio_client(StdioServerParameters(command="mcp-server-time")) as (read, write):
        async with ClientSession(read, write) as session:
            await session.initialize()
            result = await session.call_tool("get_current_time", {"timezone": zone})
    open("/work/outputs/time.json", "w").write(result.content[0].text)


asyncio.run(main())
