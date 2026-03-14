#pragma once

#include "CoreMinimal.h"
#include "Kismet/BlueprintFunctionLibrary.h"
#include "EdenBPLibrary.generated.h"

UCLASS()
class EDEN_API UEdenBPLibrary : public UBlueprintFunctionLibrary
{
	GENERATED_BODY()

public:
	UFUNCTION(BlueprintCallable, Category = "Eden|Core")
	static void StartEdenEngine(FString GameID);

	UFUNCTION(BlueprintCallable, Category = "Eden|Core")
	static void StopEdenEngine();

	UFUNCTION(BlueprintPure, Category = "Eden|Core")
	static FString GetLocalPeerID();

	UFUNCTION(BlueprintCallable, Category = "Eden|Matchmaking")
	static FString EnterMatchmaking(FString Mode, FString PartyList, int32 ExpectedPlayers);

	UFUNCTION(BlueprintCallable, Category = "Eden|Matchmaking")
	static void LeaveMatchmaking();

	UFUNCTION(BlueprintCallable, Category = "Eden|Matchmaking")
	static void AdvertiseHostLobby(FString Mode, FString MapName);

	UFUNCTION(BlueprintCallable, Category = "Eden|Matchmaking")
	static FString FindMatch(FString Mode, FString MapName);

	UFUNCTION(BlueprintPure, Category = "Eden|Wallet")
	static float GetBalance(FString Address);

	UFUNCTION(BlueprintCallable, Category = "Eden|Wallet")
	static bool SendEdenCoin(FString Receiver, float Amount);

	UFUNCTION(BlueprintCallable, Category = "Eden|Betting")
	static FString PlaceBet(FString MatchID, FString Team, float Amount);
};