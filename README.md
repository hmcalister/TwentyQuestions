# Twenty Questions

An implementation of Twenty Questions, a guessing game based around asking questions to an oracle to identify an object or concept.

This project implements an HTTP server to present a collection of webpages to play the game. The homepage allows a new game to be created, with the creator becoming the oracle, and allowing players to join by connecting to the same game ID. This allows for multiple games to occur simultaneously, with each game being handled by a separated goroutine. The oracle is authenticated using a JWT based on the game ID, so no other players can connect to the oracle URL and steal the game.