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