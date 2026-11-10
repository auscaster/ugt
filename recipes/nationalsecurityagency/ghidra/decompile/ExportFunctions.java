import ghidra.app.script.GhidraScript;
import ghidra.app.decompiler.DecompInterface;
import ghidra.app.decompiler.DecompileResults;
import ghidra.program.model.listing.Function;
import com.google.gson.GsonBuilder;
import com.google.gson.JsonArray;
import com.google.gson.JsonObject;
import java.io.File;
import java.io.PrintWriter;
import java.nio.charset.StandardCharsets;

public class ExportFunctions extends GhidraScript {
    @Override
    public void run() throws Exception {
        File output = new File(getScriptArgs()[0]);
        JsonObject root = new JsonObject();
        root.addProperty("input_sha256", currentProgram.getExecutableSHA256());
        root.addProperty("language", currentProgram.getLanguageID().toString());
        JsonArray functions = new JsonArray();
        JsonArray failures = new JsonArray();
        DecompInterface decompiler = new DecompInterface();
        if (!decompiler.openProgram(currentProgram)) throw new IllegalStateException("Cannot open decompiler");
        try (PrintWriter code = new PrintWriter(new File(output, "functions.c"), StandardCharsets.UTF_8)) {
            for (Function function : currentProgram.getFunctionManager().getFunctions(true)) {
                if (function.isExternal() || function.isThunk()) continue;
                monitor.checkCancelled();
                DecompileResults result = decompiler.decompileFunction(function, 30, monitor);
                if (!result.decompileCompleted() || result.getDecompiledFunction() == null) {
                    JsonObject error = new JsonObject();
                    error.addProperty("name", function.getName());
                    error.addProperty("address", function.getEntryPoint().toString());
                    error.addProperty("error", result.getErrorMessage());
                    failures.add(error);
                    continue;
                }
                String text = result.getDecompiledFunction().getC();
                JsonObject record = new JsonObject();
                record.addProperty("name", function.getName());
                record.addProperty("address", function.getEntryPoint().toString());
                record.addProperty("signature", function.getSignature().getPrototypeString(true));
                record.addProperty("code", text);
                functions.add(record);
                code.println(text);
            }
        } finally { decompiler.dispose(); }
        root.add("functions", functions);
        root.add("failures", failures);
        if (functions.size() == 0) throw new IllegalStateException("No function could be decompiled");
        try (PrintWriter json = new PrintWriter(new File(output, "functions.json"), StandardCharsets.UTF_8)) {
            json.println(new GsonBuilder().setPrettyPrinting().create().toJson(root));
        }
    }
}
