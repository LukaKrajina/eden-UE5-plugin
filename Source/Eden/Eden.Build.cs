/*
    Eden for Unreal Engine
    Copyright (C) 2026 LukaKrajina

    This program is free software: you can redistribute it and/or modify
    it under the terms of the GNU General Public License as published by
    the Free Software Foundation, either version 3 of the License, or
    (at your option) any later version.

    This program is distributed in the hope that it will be useful,
    but WITHOUT ANY WARRANTY; without even the implied warranty of
    MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
    GNU General Public License for more details.

    You should have received a copy of the GNU General Public License
    along with this program.  If not, see <https://www.gnu.org/licenses/>.
*/
using UnrealBuildTool;
using System.IO;

public class Eden : ModuleRules
{
	public Eden(ReadOnlyTargetRules Target) : base(Target)
	{
		PCHUsage = ModuleRules.PCHUsageMode.UseExplicitOrSharedPCHs;

		PublicDependencyModuleNames.AddRange(
			new string[]
			{
				"Core",
				"CoreUObject",
				"Engine",
				"Projects"
			}
		);
		string PluginBinariesDir = Path.Combine(PluginDirectory, "Binaries", "Win64");
		RuntimeDependencies.Add(Path.Combine(PluginBinariesDir, "UE5adam.dll"));
		RuntimeDependencies.Add(Path.Combine(PluginBinariesDir, "UE5cain.dll"));
		RuntimeDependencies.Add(Path.Combine(PluginBinariesDir, "wintun.dll"));
	}
}