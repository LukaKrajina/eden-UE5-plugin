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