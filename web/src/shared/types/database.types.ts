export type Json =
  | string
  | number
  | boolean
  | null
  | { [key: string]: Json | undefined }
  | Json[]

export type Database = {
  graphql_public: {
    Tables: {
      [_ in never]: never
    }
    Views: {
      [_ in never]: never
    }
    Functions: {
      graphql: {
        Args: {
          extensions?: Json
          operationName?: string
          query?: string
          variables?: Json
        }
        Returns: Json
      }
    }
    Enums: {
      [_ in never]: never
    }
    CompositeTypes: {
      [_ in never]: never
    }
  }
  public: {
    Tables: {
      agent_heartbeats: {
        Row: {
          blocked_sessions: number
          hostname: string
          last_seen_at: string
          machine_id: string
          user_id: string
          version: string
        }
        Insert: {
          blocked_sessions?: number
          hostname: string
          last_seen_at?: string
          machine_id: string
          user_id: string
          version: string
        }
        Update: {
          blocked_sessions?: number
          hostname?: string
          last_seen_at?: string
          machine_id?: string
          user_id?: string
          version?: string
        }
        Relationships: []
      }
      ai_usage: {
        Row: {
          cost_usd: number
          created_at: string
          id: number
          input_tokens: number
          model: string
          output_tokens: number
          project_id: string
          session_id: string | null
        }
        Insert: {
          cost_usd: number
          created_at?: string
          id?: number
          input_tokens: number
          model: string
          output_tokens: number
          project_id: string
          session_id?: string | null
        }
        Update: {
          cost_usd?: number
          created_at?: string
          id?: number
          input_tokens?: number
          model?: string
          output_tokens?: number
          project_id?: string
          session_id?: string | null
        }
        Relationships: [
          {
            foreignKeyName: "ai_usage_project_id_fkey"
            columns: ["project_id"]
            isOneToOne: false
            referencedRelation: "projects"
            referencedColumns: ["id"]
          },
          {
            foreignKeyName: "ai_usage_session_id_fkey"
            columns: ["session_id"]
            isOneToOne: false
            referencedRelation: "sessions"
            referencedColumns: ["id"]
          },
        ]
      }
      audit_log: {
        Row: {
          action: string
          actor: string | null
          at: string
          id: number
          ip: string | null
          project_id: string | null
          reason: string | null
          session_id: string | null
          user_agent: string | null
        }
        Insert: {
          action: string
          actor?: string | null
          at?: string
          id?: number
          ip?: string | null
          project_id?: string | null
          reason?: string | null
          session_id?: string | null
          user_agent?: string | null
        }
        Update: {
          action?: string
          actor?: string | null
          at?: string
          id?: number
          ip?: string | null
          project_id?: string | null
          reason?: string | null
          session_id?: string | null
          user_agent?: string | null
        }
        Relationships: []
      }
      code_snapshots: {
        Row: {
          head_sha: string
          object_key: string | null
          seq: number
          session_id: string
          taken_at: string
          tree_sha: string | null
          wip_ref: string | null
        }
        Insert: {
          head_sha: string
          object_key?: string | null
          seq: number
          session_id: string
          taken_at?: string
          tree_sha?: string | null
          wip_ref?: string | null
        }
        Update: {
          head_sha?: string
          object_key?: string | null
          seq?: number
          session_id?: string
          taken_at?: string
          tree_sha?: string | null
          wip_ref?: string | null
        }
        Relationships: [
          {
            foreignKeyName: "code_snapshots_session_id_fkey"
            columns: ["session_id"]
            isOneToOne: false
            referencedRelation: "sessions"
            referencedColumns: ["id"]
          },
        ]
      }
      project_members: {
        Row: {
          project_id: string
          role: Database["public"]["Enums"]["project_role"]
          source: string
          user_id: string
        }
        Insert: {
          project_id: string
          role: Database["public"]["Enums"]["project_role"]
          source?: string
          user_id: string
        }
        Update: {
          project_id?: string
          role?: Database["public"]["Enums"]["project_role"]
          source?: string
          user_id?: string
        }
        Relationships: [
          {
            foreignKeyName: "project_members_project_id_fkey"
            columns: ["project_id"]
            isOneToOne: false
            referencedRelation: "projects"
            referencedColumns: ["id"]
          },
        ]
      }
      project_notification_settings: {
        Row: {
          notify_digest: boolean
          project_id: string
          slack_webhook_url: string | null
        }
        Insert: {
          notify_digest?: boolean
          project_id: string
          slack_webhook_url?: string | null
        }
        Update: {
          notify_digest?: boolean
          project_id?: string
          slack_webhook_url?: string | null
        }
        Relationships: [
          {
            foreignKeyName: "project_notification_settings_project_id_fkey"
            columns: ["project_id"]
            isOneToOne: true
            referencedRelation: "projects"
            referencedColumns: ["id"]
          },
        ]
      }
      projects: {
        Row: {
          bucket: string | null
          closed_at: string | null
          daily_token_budget: number
          git_remote: string
          id: string
          name: string
          retention_days_after_close: number
          team_admin: string | null
          team_resumer: string | null
          team_viewer: string | null
        }
        Insert: {
          bucket?: string | null
          closed_at?: string | null
          daily_token_budget?: number
          git_remote: string
          id?: string
          name: string
          retention_days_after_close?: number
          team_admin?: string | null
          team_resumer?: string | null
          team_viewer?: string | null
        }
        Update: {
          bucket?: string | null
          closed_at?: string | null
          daily_token_budget?: number
          git_remote?: string
          id?: string
          name?: string
          retention_days_after_close?: number
          team_admin?: string | null
          team_resumer?: string | null
          team_viewer?: string | null
        }
        Relationships: []
      }
      redaction_config: {
        Row: {
          denylist_additions: string[]
          disabled_rule_ids: string[]
          id: string
          pinned_version: string | null
          updated_at: string
        }
        Insert: {
          denylist_additions?: string[]
          disabled_rule_ids?: string[]
          id?: string
          pinned_version?: string | null
          updated_at?: string
        }
        Update: {
          denylist_additions?: string[]
          disabled_rule_ids?: string[]
          id?: string
          pinned_version?: string | null
          updated_at?: string
        }
        Relationships: []
      }
      session_chunks: {
        Row: {
          byte_end: number
          byte_start: number
          last_rescanned_at: string | null
          object_key: string
          redactions: number
          seq: number
          session_id: string
          sha256: string
          stream_id: string
          uploaded_at: string
        }
        Insert: {
          byte_end: number
          byte_start: number
          last_rescanned_at?: string | null
          object_key: string
          redactions?: number
          seq: number
          session_id: string
          sha256: string
          stream_id?: string
          uploaded_at?: string
        }
        Update: {
          byte_end?: number
          byte_start?: number
          last_rescanned_at?: string | null
          object_key?: string
          redactions?: number
          seq?: number
          session_id?: string
          sha256?: string
          stream_id?: string
          uploaded_at?: string
        }
        Relationships: [
          {
            foreignKeyName: "session_chunks_session_id_fkey"
            columns: ["session_id"]
            isOneToOne: false
            referencedRelation: "sessions"
            referencedColumns: ["id"]
          },
        ]
      }
      session_passages: {
        Row: {
          created_at: string
          embedding: string
          id: number
          seq: number
          session_id: string
          text: string
        }
        Insert: {
          created_at?: string
          embedding: string
          id?: number
          seq: number
          session_id: string
          text: string
        }
        Update: {
          created_at?: string
          embedding?: string
          id?: number
          seq?: number
          session_id?: string
          text?: string
        }
        Relationships: [
          {
            foreignKeyName: "session_passages_session_id_fkey"
            columns: ["session_id"]
            isOneToOne: false
            referencedRelation: "sessions"
            referencedColumns: ["id"]
          },
        ]
      }
      sessions: {
        Row: {
          blocked_reason: string | null
          branch: string | null
          claude_code_version: string | null
          embedding: string | null
          ended_reason: string | null
          files_touched: string[] | null
          head_sha: string | null
          id: string
          last_activity_at: string
          last_resummarize_requested_at: string | null
          machine: string | null
          original_cwd: string
          outcome: string | null
          parent_session_id: string | null
          pinned: boolean
          project_id: string
          search_vector: unknown
          started_at: string
          status: string
          summarized_at: string | null
          summarized_through_seq: number | null
          summary: string | null
          title: string | null
          title_source: string
          total_bytes: number
          user_id: string
        }
        Insert: {
          blocked_reason?: string | null
          branch?: string | null
          claude_code_version?: string | null
          embedding?: string | null
          ended_reason?: string | null
          files_touched?: string[] | null
          head_sha?: string | null
          id: string
          last_activity_at?: string
          last_resummarize_requested_at?: string | null
          machine?: string | null
          original_cwd: string
          outcome?: string | null
          parent_session_id?: string | null
          pinned?: boolean
          project_id: string
          search_vector?: unknown
          started_at?: string
          status?: string
          summarized_at?: string | null
          summarized_through_seq?: number | null
          summary?: string | null
          title?: string | null
          title_source?: string
          total_bytes?: number
          user_id: string
        }
        Update: {
          blocked_reason?: string | null
          branch?: string | null
          claude_code_version?: string | null
          embedding?: string | null
          ended_reason?: string | null
          files_touched?: string[] | null
          head_sha?: string | null
          id?: string
          last_activity_at?: string
          last_resummarize_requested_at?: string | null
          machine?: string | null
          original_cwd?: string
          outcome?: string | null
          parent_session_id?: string | null
          pinned?: boolean
          project_id?: string
          search_vector?: unknown
          started_at?: string
          status?: string
          summarized_at?: string | null
          summarized_through_seq?: number | null
          summary?: string | null
          title?: string | null
          title_source?: string
          total_bytes?: number
          user_id?: string
        }
        Relationships: [
          {
            foreignKeyName: "sessions_parent_session_id_fkey"
            columns: ["parent_session_id"]
            isOneToOne: false
            referencedRelation: "sessions"
            referencedColumns: ["id"]
          },
          {
            foreignKeyName: "sessions_project_id_fkey"
            columns: ["project_id"]
            isOneToOne: false
            referencedRelation: "projects"
            referencedColumns: ["id"]
          },
        ]
      }
    }
    Views: {
      [_ in never]: never
    }
    Functions: {
      end_idle_sessions: { Args: never; Returns: undefined }
      find_sessions_by_file: {
        Args: { p_limit?: number; p_path: string; p_project_id?: string }
        Returns: {
          blocked_reason: string | null
          branch: string | null
          claude_code_version: string | null
          embedding: string | null
          ended_reason: string | null
          files_touched: string[] | null
          head_sha: string | null
          id: string
          last_activity_at: string
          last_resummarize_requested_at: string | null
          machine: string | null
          original_cwd: string
          outcome: string | null
          parent_session_id: string | null
          pinned: boolean
          project_id: string
          search_vector: unknown
          started_at: string
          status: string
          summarized_at: string | null
          summarized_through_seq: number | null
          summary: string | null
          title: string | null
          title_source: string
          total_bytes: number
          user_id: string
        }[]
        SetofOptions: {
          from: "*"
          to: "sessions"
          isOneToOne: false
          isSetofReturn: true
        }
      }
      has_role: {
        Args: {
          p_min_role: Database["public"]["Enums"]["project_role"]
          p_project_id: string
        }
        Returns: boolean
      }
      is_any_project_admin: { Args: never; Returns: boolean }
      search_sessions: {
        Args: {
          p_match_count?: number
          p_project_id?: string
          p_query_embedding: string
          p_query_text: string
        }
        Returns: {
          matching_passage: string
          score: number
          session_id: string
        }[]
      }
      storage_object_project_id: {
        Args: { object_name: string }
        Returns: string
      }
    }
    Enums: {
      project_role: "viewer" | "resumer" | "admin"
    }
    CompositeTypes: {
      [_ in never]: never
    }
  }
}

type DatabaseWithoutInternals = Omit<Database, "__InternalSupabase">

type DefaultSchema = DatabaseWithoutInternals[Extract<keyof Database, "public">]

export type Tables<
  DefaultSchemaTableNameOrOptions extends
    | keyof (DefaultSchema["Tables"] & DefaultSchema["Views"])
    | { schema: keyof DatabaseWithoutInternals },
  TableName extends DefaultSchemaTableNameOrOptions extends {
    schema: keyof DatabaseWithoutInternals
  }
    ? keyof (DatabaseWithoutInternals[DefaultSchemaTableNameOrOptions["schema"]]["Tables"] &
        DatabaseWithoutInternals[DefaultSchemaTableNameOrOptions["schema"]]["Views"])
    : never = never,
> = DefaultSchemaTableNameOrOptions extends {
  schema: keyof DatabaseWithoutInternals
}
  ? (DatabaseWithoutInternals[DefaultSchemaTableNameOrOptions["schema"]]["Tables"] &
      DatabaseWithoutInternals[DefaultSchemaTableNameOrOptions["schema"]]["Views"])[TableName] extends {
      Row: infer R
    }
    ? R
    : never
  : DefaultSchemaTableNameOrOptions extends keyof (DefaultSchema["Tables"] &
        DefaultSchema["Views"])
    ? (DefaultSchema["Tables"] &
        DefaultSchema["Views"])[DefaultSchemaTableNameOrOptions] extends {
        Row: infer R
      }
      ? R
      : never
    : never

export type TablesInsert<
  DefaultSchemaTableNameOrOptions extends
    | keyof DefaultSchema["Tables"]
    | { schema: keyof DatabaseWithoutInternals },
  TableName extends DefaultSchemaTableNameOrOptions extends {
    schema: keyof DatabaseWithoutInternals
  }
    ? keyof DatabaseWithoutInternals[DefaultSchemaTableNameOrOptions["schema"]]["Tables"]
    : never = never,
> = DefaultSchemaTableNameOrOptions extends {
  schema: keyof DatabaseWithoutInternals
}
  ? DatabaseWithoutInternals[DefaultSchemaTableNameOrOptions["schema"]]["Tables"][TableName] extends {
      Insert: infer I
    }
    ? I
    : never
  : DefaultSchemaTableNameOrOptions extends keyof DefaultSchema["Tables"]
    ? DefaultSchema["Tables"][DefaultSchemaTableNameOrOptions] extends {
        Insert: infer I
      }
      ? I
      : never
    : never

export type TablesUpdate<
  DefaultSchemaTableNameOrOptions extends
    | keyof DefaultSchema["Tables"]
    | { schema: keyof DatabaseWithoutInternals },
  TableName extends DefaultSchemaTableNameOrOptions extends {
    schema: keyof DatabaseWithoutInternals
  }
    ? keyof DatabaseWithoutInternals[DefaultSchemaTableNameOrOptions["schema"]]["Tables"]
    : never = never,
> = DefaultSchemaTableNameOrOptions extends {
  schema: keyof DatabaseWithoutInternals
}
  ? DatabaseWithoutInternals[DefaultSchemaTableNameOrOptions["schema"]]["Tables"][TableName] extends {
      Update: infer U
    }
    ? U
    : never
  : DefaultSchemaTableNameOrOptions extends keyof DefaultSchema["Tables"]
    ? DefaultSchema["Tables"][DefaultSchemaTableNameOrOptions] extends {
        Update: infer U
      }
      ? U
      : never
    : never

export type Enums<
  DefaultSchemaEnumNameOrOptions extends
    | keyof DefaultSchema["Enums"]
    | { schema: keyof DatabaseWithoutInternals },
  EnumName extends DefaultSchemaEnumNameOrOptions extends {
    schema: keyof DatabaseWithoutInternals
  }
    ? keyof DatabaseWithoutInternals[DefaultSchemaEnumNameOrOptions["schema"]]["Enums"]
    : never = never,
> = DefaultSchemaEnumNameOrOptions extends {
  schema: keyof DatabaseWithoutInternals
}
  ? DatabaseWithoutInternals[DefaultSchemaEnumNameOrOptions["schema"]]["Enums"][EnumName]
  : DefaultSchemaEnumNameOrOptions extends keyof DefaultSchema["Enums"]
    ? DefaultSchema["Enums"][DefaultSchemaEnumNameOrOptions]
    : never

export type CompositeTypes<
  PublicCompositeTypeNameOrOptions extends
    | keyof DefaultSchema["CompositeTypes"]
    | { schema: keyof DatabaseWithoutInternals },
  CompositeTypeName extends PublicCompositeTypeNameOrOptions extends {
    schema: keyof DatabaseWithoutInternals
  }
    ? keyof DatabaseWithoutInternals[PublicCompositeTypeNameOrOptions["schema"]]["CompositeTypes"]
    : never = never,
> = PublicCompositeTypeNameOrOptions extends {
  schema: keyof DatabaseWithoutInternals
}
  ? DatabaseWithoutInternals[PublicCompositeTypeNameOrOptions["schema"]]["CompositeTypes"][CompositeTypeName]
  : PublicCompositeTypeNameOrOptions extends keyof DefaultSchema["CompositeTypes"]
    ? DefaultSchema["CompositeTypes"][PublicCompositeTypeNameOrOptions]
    : never

export const Constants = {
  graphql_public: {
    Enums: {},
  },
  public: {
    Enums: {
      project_role: ["viewer", "resumer", "admin"],
    },
  },
} as const

