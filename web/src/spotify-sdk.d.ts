// The slice of the Spotify Web Playback SDK this app uses.
// https://developer.spotify.com/documentation/web-playback-sdk/reference

declare namespace Spotify {
  type SdkArtist = { name: string; uri: string };
  type SdkTrack = {
    uri: string;
    id: string | null;
    name: string;
    duration_ms: number;
    artists: SdkArtist[];
    album: { name: string; uri: string };
  };
  type PlaybackState = {
    paused: boolean;
    loading?: boolean;
    position: number;
    duration: number;
    shuffle: boolean;
    repeat_mode: 0 | 1 | 2;
    track_window: { current_track: SdkTrack | null };
  };
  type ErrorEvent = { message: string };
  type ReadyEvent = { device_id: string };

  class Player {
    constructor(options: {
      name: string;
      getOAuthToken: (cb: (token: string) => void) => void;
      volume?: number;
    });
    connect(): Promise<boolean>;
    disconnect(): void;
    addListener(event: "ready" | "not_ready", cb: (e: ReadyEvent) => void): boolean;
    addListener(event: "player_state_changed", cb: (s: PlaybackState | null) => void): boolean;
    addListener(
      event: "initialization_error" | "authentication_error" | "account_error" | "playback_error",
      cb: (e: ErrorEvent) => void,
    ): boolean;
    getCurrentState(): Promise<PlaybackState | null>;
    setVolume(volume: number): Promise<void>;
    togglePlay(): Promise<void>;
    seek(positionMs: number): Promise<void>;
    previousTrack(): Promise<void>;
    nextTrack(): Promise<void>;
    activateElement(): Promise<void>;
  }
}

interface Window {
  onSpotifyWebPlaybackSDKReady?: () => void;
  Spotify?: typeof Spotify;
}
